package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	cfg := LoadConfig()

	store := NewJobStore(time.Duration(cfg.JobTTLSec) * time.Second)
	whisperClient := NewWhisperClient(cfg)
	queue := NewJobQueue(cfg, store, whisperClient)

	cleanupStop := make(chan struct{})
	go store.StartCleanup(time.Duration(cfg.JobCleanupIntervalSec)*time.Second, cleanupStop)

	queue.Start()

	limiter := NewRateLimiter(cfg)
	conv := NewConverter(cfg)
	handler := buildHandler(cfg, limiter, conv, queue, store)

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
		go func() { queue.wg.Wait(); close(done) }()
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
