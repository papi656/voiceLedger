package server

import "net/http"

// BodySizeLimitMiddleware caps the request body size to maxMB megabytes.
func BodySizeLimitMiddleware(maxMB int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(nil, r.Body, int64(maxMB)*1024*1024)
			next.ServeHTTP(w, r)
		})
	}
}
