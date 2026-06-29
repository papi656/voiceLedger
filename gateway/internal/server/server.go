package server

import (
	"encoding/json"
	"net/http"

	"gateway/internal/audio"
	"gateway/internal/auth"
	"gateway/internal/config"
	"gateway/internal/ratelimit"
	"gateway/internal/transcription"
)

// BuildHandler constructs the HTTP handler with all routes and middleware.
func BuildHandler(
	cfg *config.Config,
	limiter *ratelimit.RateLimiter,
	conv *audio.Converter,
	queue *transcription.JobQueue,
	store *transcription.JobStore,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler())

	keyFn := func(r *http.Request) string {
		if user, ok := auth.UserFromContext(r.Context()); ok {
			return user.Sub
		}
		return "anonymous"
	}

	jobHandler := transcription.SubmitJobHandler(cfg, conv, queue, store)
	jobHandler = ratelimit.RateLimitMiddleware(limiter, keyFn)(jobHandler)
	jobHandler = auth.AuthMiddleware(cfg.OAuthAudience)(jobHandler)
	mux.Handle("POST /jobs", jobHandler)

	statusHandler := transcription.JobStatusHandler(store)
	statusHandler = ratelimit.RateLimitMiddleware(limiter, keyFn)(statusHandler)
	statusHandler = auth.AuthMiddleware(cfg.OAuthAudience)(statusHandler)
	mux.Handle("GET /jobs/", statusHandler)

	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})
	mux.Handle("/", ratelimit.RateLimitMiddleware(limiter, keyFn)(auth.AuthMiddleware(cfg.OAuthAudience)(catchAll)))

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
