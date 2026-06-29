package server

import (
	"net/http"
	"strings"
)

// CORSMiddleware adds CORS headers and handles preflight (OPTIONS) requests.
// It must be the outermost middleware so preflight never reaches auth.
// allowedOrigins can be "*" for development or a comma-separated list of origins.
func CORSMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if allowedOrigin(origin, allowedOrigins) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Sheets-Token")
			w.Header().Set("Access-Control-Expose-Headers", "X-RateLimit-Remaining-Key, X-RateLimit-Remaining-IP, Retry-After")
			w.Header().Set("Access-Control-Max-Age", "86400")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func allowedOrigin(origin, allowed string) bool {
	if allowed == "*" {
		return true
	}
	for _, o := range strings.Split(allowed, ",") {
		if strings.TrimSpace(o) == origin {
			return true
		}
	}
	return false
}

// BodySizeLimitMiddleware caps the request body size to maxMB megabytes.
func BodySizeLimitMiddleware(maxMB int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(nil, r.Body, int64(maxMB)*1024*1024)
			next.ServeHTTP(w, r)
		})
	}
}
