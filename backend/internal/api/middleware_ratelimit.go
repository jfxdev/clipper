package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// idleBucketTTL bounds memory usage: buckets for IPs that haven't made a
// request in this long are evicted by the periodic cleanup.
const idleBucketTTL = 10 * time.Minute

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter applies a per-IP token bucket. Each unique client IP gets its
// own bucket, created lazily on first request.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rps        rate.Limit
	burst      int
	trustProxy bool
}

func NewRateLimiter(rps float64, burst int, trustProxy bool) *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*bucket),
		rps:        rate.Limit(rps),
		burst:      burst,
		trustProxy: trustProxy,
	}
	go rl.evictIdleLoop()
	return rl
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, rl.trustProxy)
		if !rl.allow(ip) {
			writeError(w, ErrRateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{limiter: rate.NewLimiter(rl.rps, rl.burst)}
		rl.buckets[ip] = b
	}
	b.lastSeen = time.Now()
	limiter := b.limiter
	rl.mu.Unlock()
	return limiter.Allow()
}

func (rl *RateLimiter) evictIdleLoop() {
	ticker := time.NewTicker(idleBucketTTL)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-idleBucketTTL)
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// clientIP extracts the request's client IP. X-Forwarded-For is only
// trusted when trustProxy is set (i.e. the app is known to sit behind a
// reverse proxy that sets the header itself) — otherwise a client could
// spoof the header to bypass rate limiting entirely.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			first, _, _ := strings.Cut(fwd, ",")
			return strings.TrimSpace(first)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
