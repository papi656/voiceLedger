package main

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

type RateLimiter struct {
	mu         sync.Mutex
	keyBuckets map[string]*tokenBucket
	ipBuckets  map[string]*tokenBucket
	keyRate    float64
	keyBurst   int
	ipRate     float64
	ipBurst    int
}

func NewRateLimiter(cfg *Config) *RateLimiter {
	rl := &RateLimiter{
		keyBuckets: make(map[string]*tokenBucket),
		ipBuckets:  make(map[string]*tokenBucket),
		keyRate:    float64(cfg.RateLimitPerKey) / 60.0,
		keyBurst:   cfg.RateBurstPerKey,
		ipRate:     float64(cfg.RateLimitPerIP) / 60.0,
		ipBurst:    cfg.RateBurstPerIP,
	}
	go rl.cleanup(10 * time.Minute, 30*time.Minute)
	return rl
}

func (rl *RateLimiter) allow(keyID, ip string) (keyOK bool, keyRemaining int, ipOK bool, ipRemaining int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	keyOK, keyRemaining = rl.checkBucket(rl.keyBuckets, keyID, rl.keyRate, rl.keyBurst)
	ipOK, ipRemaining = rl.checkBucket(rl.ipBuckets, ip, rl.ipRate, rl.ipBurst)
	return
}

func (rl *RateLimiter) checkBucket(buckets map[string]*tokenBucket, id string, rate float64, burst int) (bool, int) {
	b, exists := buckets[id]
	if !exists {
		b = &tokenBucket{
			tokens:   float64(burst),
			burst:    burst,
			rate:     rate,
			lastFill: time.Now(),
			lastUsed: time.Now(),
		}
		buckets[id] = b
	}

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * rate
	if b.tokens > float64(burst) {
		b.tokens = float64(burst)
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
		for id, b := range rl.keyBuckets {
			if b.lastUsed.Before(cutoff) {
				delete(rl.keyBuckets, id)
			}
		}
		for id, b := range rl.ipBuckets {
			if b.lastUsed.Before(cutoff) {
				delete(rl.ipBuckets, id)
			}
		}
		rl.mu.Unlock()
	}
}

func clientIP(r *http.Request) string {
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

func rateLimitMiddleware(limiter *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keyID := "anonymous"
			if v := r.Context().Value(keyIDKey); v != nil {
				if user, ok := v.(UserInfo); ok {
					keyID = user.Sub
				}
			}
			ip := clientIP(r)

			keyOK, keyRem, ipOK, ipRem := limiter.allow(keyID, ip)

			w.Header().Set("X-RateLimit-Limit-Key", "N/A")
			w.Header().Set("X-RateLimit-Remaining-Key", "N/A")
			w.Header().Set("X-RateLimit-Limit-IP", "N/A")
			w.Header().Set("X-RateLimit-Remaining-IP", "N/A")

			if !keyOK || !ipOK {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded, retry after 60 seconds",
				})
				return
			}
			_ = keyRem
			_ = ipRem

			next.ServeHTTP(w, r)
		})
	}
}
