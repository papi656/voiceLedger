package ratelimit

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens   float64
	burst    int
	rate     float64
	lastFill time.Time
	lastUsed time.Time
}

// RateLimiter is an IP-based token-bucket rate limiter.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64
	burst    int
}

// NewRateLimiter creates a rate limiter with the given per-IP limits.
// Rate is expressed as tokens-per-second; burst is the maximum bucket size.
func NewRateLimiter(ratePerSec float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*tokenBucket),
		rate:    ratePerSec,
		burst:   burst,
	}
	go rl.cleanup(10*time.Minute, 30*time.Minute)
	return rl
}

// Allow checks whether the given IP is allowed to proceed.
func (rl *RateLimiter) Allow(ip string) (ok bool, remaining int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, exists := rl.buckets[ip]
	if !exists {
		b = &tokenBucket{
			tokens:   float64(rl.burst),
			burst:    rl.burst,
			rate:     rl.rate,
			lastFill: time.Now(),
			lastUsed: time.Now(),
		}
		rl.buckets[ip] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastFill = now
	b.lastUsed = now

	if b.tokens < 1 {
		return false, 0
	}
	b.tokens--
	return true, int(b.tokens)
}

func (rl *RateLimiter) cleanup(interval, maxAge time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-maxAge)
		for id, b := range rl.buckets {
			if b.lastUsed.Before(cutoff) {
				delete(rl.buckets, id)
			}
		}
		rl.mu.Unlock()
	}
}

// ClientIP extracts the client IP from the request, respecting X-Forwarded-For.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		return strings.TrimSpace(parts[0])
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.Trim(real, " ")
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// RateLimitMiddleware returns middleware that enforces per-IP rate limits.
func RateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := ClientIP(r)

			ok, remaining := limiter.Allow(ip)

			w.Header().Set("X-RateLimit-Limit-IP", "N/A")
			w.Header().Set("X-RateLimit-Remaining-IP", "N/A")

			if !ok {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded, retry after 60 seconds",
				})
				return
			}
			_ = remaining

			next.ServeHTTP(w, r)
		})
	}
}
