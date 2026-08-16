package server

import (
	"encoding/json"
	"net/http"

	"gateway/internal/audio"
	"gateway/internal/config"
	"gateway/internal/ratelimit"
	"gateway/internal/sheets"
	"gateway/internal/transcription"
)

// BuildHandler constructs the HTTP handler with all routes and middleware.
func BuildHandler(
	cfg *config.Config,
	limiter *ratelimit.RateLimiter,
	conv *audio.Converter,
	queue *transcription.JobQueue,
	store *transcription.JobStore,
	sheetsClient *sheets.Client,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler())

	jobHandler := transcription.SubmitJobHandler(cfg, conv, queue, store, sheetsClient)
	jobHandler = ratelimit.RateLimitMiddleware(limiter)(jobHandler)
	mux.Handle("POST /jobs", jobHandler)

	statusHandler := transcription.JobStatusHandler(store)
	statusHandler = ratelimit.RateLimitMiddleware(limiter)(statusHandler)
	mux.Handle("GET /jobs/", statusHandler)

	sheetsHandler := transcription.ListSheetsHandler(sheetsClient)
	sheetsHandler = ratelimit.RateLimitMiddleware(limiter)(sheetsHandler)
	mux.Handle("GET /sheets", sheetsHandler)

	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})
	mux.Handle("/", ratelimit.RateLimitMiddleware(limiter)(catchAll))

	handler := BodySizeLimitMiddleware(cfg.MaxBodySizeMB)(mux)
	return CORSMiddleware(cfg.CORSAllowedOrigins)(handler)
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"service": "whisper-gateway",
			"version": "1.0.0",
		})
	}
}
