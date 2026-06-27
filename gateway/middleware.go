package main

import (
	"net/http"
)

func buildHandler(cfg *Config, limiter *RateLimiter, conv *Converter, queue *JobQueue, store *JobStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler())

	jobHandler := submitJobHandler(cfg, conv, queue, store)
	jobHandler = rateLimitMiddleware(limiter)(jobHandler)
	jobHandler = authMiddleware(cfg)(jobHandler)
	mux.Handle("POST /jobs", jobHandler)

	statusHandler := jobStatusHandler(store)
	statusHandler = rateLimitMiddleware(limiter)(statusHandler)
	statusHandler = authMiddleware(cfg)(statusHandler)
	mux.Handle("GET /jobs/", statusHandler)

	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	})
	mux.Handle("/", rateLimitMiddleware(limiter)(authMiddleware(cfg)(catchAll)))

	return bodySizeLimitMiddleware(cfg)(mux)
}

func bodySizeLimitMiddleware(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(nil, r.Body, int64(cfg.MaxBodySizeMB)*1024*1024)
			next.ServeHTTP(w, r)
		})
	}
}
