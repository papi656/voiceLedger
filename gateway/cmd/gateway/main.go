package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gateway/internal/audio"
	"gateway/internal/config"
	"gateway/internal/llm"
	"gateway/internal/ratelimit"
	"gateway/internal/server"
	"gateway/internal/sheets"
	"gateway/internal/transcription"
	"gateway/internal/whisper"
)

func main() {
	cfg := config.Load()

	store := transcription.NewJobStore(time.Duration(cfg.JobTTLSec) * time.Second)
	whisperClient := whisper.NewClient(
		cfg.WhisperHost,
		cfg.WhisperPort,
		time.Duration(cfg.WhisperTimeoutSec)*time.Second,
	)

	// Sheets client first — its Type dropdown values feed the LLM prompt.
	var sheetsClient *sheets.Client
	var sheetCategories []string
	if cfg.GoogleSheetsEnabled {
		sc, err := sheets.NewClient(
			cfg.GoogleSheetsKeyPath,
			cfg.GoogleSheetsSheetID,
			cfg.GoogleSheetsTab,
			time.Duration(cfg.GoogleSheetsTimeoutSec)*time.Second,
		)
		if err != nil {
			log.Fatalf("google sheets client init failed: %v", err)
		}
		sheetsClient = sc
		log.Printf("google sheets export enabled: sheet=%s tab=%s", cfg.GoogleSheetsSheetID, cfg.GoogleSheetsTab)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cats, err := sc.TypeCategories(ctx, cfg.GoogleSheetsTab)
		cancel()
		if err != nil {
			log.Printf("could not load sheet type categories (LLM will use a generic prompt): %v", err)
		} else if len(cats) > 0 {
			sheetCategories = cats
			log.Printf("loaded %d sheet type categories: %v", len(cats), cats)
		}
	}

	llmClient := llm.NewClient(
		cfg.LLMHost,
		cfg.LLMPort,
		time.Duration(cfg.LLMTimeoutSec)*time.Second,
		sheetCategories,
	)

	queue := transcription.NewJobQueue(
		cfg.MaxQueueSize,
		cfg.NumWorkers,
		time.Duration(cfg.WhisperTimeoutSec)*time.Second,
		store,
		whisperClient,
		llmClient,
		cfg.LLMMaxRetries,
		sheetsClient,
		cfg.GoogleSheetsMaxRetries,
	)

	cleanupStop := make(chan struct{})
	go store.StartCleanup(time.Duration(cfg.JobCleanupIntervalSec)*time.Second, cleanupStop)

	queue.Start()

	limiter := ratelimit.NewRateLimiter(
		float64(cfg.RateLimitPerIP)/60.0,
		cfg.RateBurstPerIP,
	)

	conv := audio.NewConverter(
		cfg.FFMPEGPath,
		time.Duration(cfg.ConvertTimeoutSec)*time.Second,
	)

	handler := server.BuildHandler(cfg, limiter, conv, queue, store, sheetsClient)

	srv := &http.Server{
		Addr:         ":" + cfg.GatewayPort,
		Handler:      handler,
		ReadTimeout:  time.Duration(cfg.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeoutSec) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeoutSec) * time.Second,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")

		queue.Stop()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		done := make(chan struct{})
		go func() { queue.Wg.Wait(); close(done) }()
		select {
		case <-done:
			log.Println("workers drained")
		case <-ctx.Done():
			log.Println("timeout waiting for workers, forcing shutdown")
		}

		close(cleanupStop)

		if err := srv.Shutdown(context.Background()); err != nil {
			log.Printf("server shutdown error: %v", err)
		}
	}()

	log.Printf("gateway listening on :%s, %d worker(s)", cfg.GatewayPort, cfg.NumWorkers)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("gateway failed: %v", err)
	}
}
