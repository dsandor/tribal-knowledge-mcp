package web

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
	lastSeen time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rps     int
}

// NewRateLimiter returns a chi-compatible per-IP token bucket middleware.
// rps is the number of requests permitted per second per IP.
// trustXFF controls whether the X-Forwarded-For header is trusted for IP
// extraction. Only set this to true when the server runs behind a known
// reverse proxy (controlled by TRUST_XFF env var) — clients can spoof XFF
// when there is no trusted proxy in front.
// A background goroutine prunes buckets idle for more than 5 minutes.
func NewRateLimiter(rps int, trustXFF bool) func(http.Handler) http.Handler {
	rl := &rateLimiter{
		buckets: make(map[string]*tokenBucket),
		rps:     rps,
	}

	go rl.prune()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := extractIP(r, trustXFF)
			if !rl.allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "rate limit exceeded",
					"code":  "rate_limited",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// allow checks the token bucket for the given IP and consumes one token if
// available. Returns true when the request is permitted.
func (rl *rateLimiter) allow(ip string) bool {
	now := time.Now()
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	if !ok {
		b = &tokenBucket{tokens: float64(rl.rps), lastSeen: now}
		rl.buckets[ip] = b
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += float64(rl.rps) * elapsed
	if b.tokens > float64(rl.rps) {
		b.tokens = float64(rl.rps)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// prune removes buckets that have been idle for more than 5 minutes.
func (rl *rateLimiter) prune() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-5 * time.Minute)
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// extractIP returns the client IP address.
// When trustXFF is false (default), r.RemoteAddr is always used — XFF is
// ignored because clients can set it to any value.
// When trustXFF is true (opt-in for deployments behind a known reverse proxy),
// the rightmost entry in X-Forwarded-For is used; the rightmost value is
// appended by your own infrastructure and is therefore the hardest to spoof.
func extractIP(r *http.Request, trustXFF bool) string {
	if trustXFF {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Use the rightmost entry — added by the last proxy, which is
			// your own infrastructure. Unlike the leftmost, it cannot be
			// freely controlled by the client.
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
