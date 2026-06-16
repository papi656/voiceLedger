package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const keyIDKey contextKey = "key_id"

func authMiddleware(cfg *Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(cfg.APIKeys) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			header := r.Header.Get("Authorization")

			if header == "" {
				writeAuthError(w, "missing Authorization header")
				return
			}

			if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
				writeAuthError(w, "invalid Authorization format, expected Bearer")
				return
			}

			token := strings.TrimSpace(header[7:]) // strip "Bearer "
			if token == "" {
				writeAuthError(w, "empty token")
				return
			}

			for _, k := range cfg.APIKeys {
				if token == k {
					prefix := token
					if len(prefix) > 8 {
						prefix = prefix[:8]
					}
					ctx := context.WithValue(r.Context(), keyIDKey, prefix)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			writeAuthError(w, "unauthorized")
		})
	}
}

func writeAuthError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
