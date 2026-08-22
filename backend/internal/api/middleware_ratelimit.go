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

	stop chan struct{}
	once sync.Once
}

func NewRateLimiter(rps float64, burst int, trustProxy bool) *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*bucket),
		rps:        rate.Limit(rps),
		burst:      burst,
		trustProxy: trustProxy,
		stop:       make(chan struct{}),
	}
	go rl.evictIdleLoop()
	return rl
}

// Close stops the background idle-bucket eviction goroutine. Safe to call
// more than once.
func (rl *RateLimiter) Close() {
	rl.once.Do(func() { close(rl.stop) })
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
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
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
}

// clientIP extracts the request's client IP. X-Forwarded-For is only
// trusted when trustProxy is set (i.e. the app is known to sit behind a
// single trusted reverse proxy that sets the header itself). Each proxy in
// the chain *appends* to X-Forwarded-For ("client, proxy1, proxy2, ..."),
// so the rightmost entry is the one our directly-trusted proxy actually
// observed — the leftmost entry is client-supplied and can be spoofed to
// any value, which would let a client rotate it to bypass rate limiting
// entirely.
func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			parts := strings.Split(fwd, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
