package api

import (
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// idleBucketTTL bounds memory usage: buckets for clients that haven't made
// a request in this long are evicted by the periodic cleanup.
const idleBucketTTL = 10 * time.Minute

// evictInterval is how often the sweep runs. It is deliberately shorter
// than idleBucketTTL so a flood of one-shot clients is reclaimed promptly
// rather than accumulating for a full TTL.
const evictInterval = time.Minute

type bucket struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiterConfig configures NewRateLimiter.
type RateLimiterConfig struct {
	RPS   float64
	Burst int

	// GlobalRPS/GlobalBurst is a ceiling shared by every client. Per-client
	// limits alone do nothing against a flood spread over thousands of
	// source addresses.
	GlobalRPS   float64
	GlobalBurst int

	// MaxClients caps the number of per-client buckets held at once. When
	// the cap is reached, new clients are served by the shared overflow
	// bucket instead of allocating: an attacker rotating through an IPv6
	// prefix can no longer grow this map without bound.
	MaxClients int

	TrustProxy     bool
	TrustedProxies []netip.Prefix
}

// RateLimiter applies a per-client token bucket, a global token bucket, and
// a bounded amount of memory to do it in.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	overflow *rate.Limiter

	rps        rate.Limit
	burst      int
	maxClients int
	global     *rate.Limiter

	trustProxy     bool
	trustedProxies []netip.Prefix

	stop chan struct{}
	once sync.Once
}

func NewRateLimiter(cfg RateLimiterConfig) *RateLimiter {
	rl := &RateLimiter{
		buckets:        make(map[string]*bucket),
		overflow:       rate.NewLimiter(rate.Limit(cfg.RPS), cfg.Burst),
		rps:            rate.Limit(cfg.RPS),
		burst:          cfg.Burst,
		maxClients:     cfg.MaxClients,
		global:         rate.NewLimiter(rate.Limit(cfg.GlobalRPS), cfg.GlobalBurst),
		trustProxy:     cfg.TrustProxy,
		trustedProxies: cfg.TrustedProxies,
		stop:           make(chan struct{}),
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
		if !rl.allow(clientIP(r, rl.trustProxy, rl.trustedProxies)) {
			retryAfter(w, rl.rps)
			writeError(w, ErrRateLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// retryAfter tells a well-behaved client when to come back, rather than
// leaving it to guess and retry immediately.
func retryAfter(w http.ResponseWriter, rps rate.Limit) {
	seconds := 1
	if rps > 0 && rps < 1 {
		seconds = int(1/float64(rps)) + 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
}

func (rl *RateLimiter) allow(key string) bool {
	// The global ceiling is checked first and consumes a token regardless
	// of which client bucket applies, so a distributed flood is capped in
	// aggregate.
	if !rl.global.Allow() {
		return false
	}
	return rl.clientLimiter(key).Allow()
}

func (rl *RateLimiter) clientLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if b, ok := rl.buckets[key]; ok {
		b.lastSeen = time.Now()
		return b.limiter
	}
	if len(rl.buckets) >= rl.maxClients {
		// Shared fallback: still rate limited, just not with per-client
		// fairness. Degrading here is the point — the alternative is
		// unbounded allocation driven by unauthenticated requests.
		return rl.overflow
	}
	b := &bucket{limiter: rate.NewLimiter(rl.rps, rl.burst), lastSeen: time.Now()}
	rl.buckets[key] = b
	return b.limiter
}

func (rl *RateLimiter) evictIdleLoop() {
	ticker := time.NewTicker(evictInterval)
	defer ticker.Stop()
	for {
		select {
		case <-rl.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-idleBucketTTL)
			rl.mu.Lock()
			for key, b := range rl.buckets {
				if b.lastSeen.Before(cutoff) {
					delete(rl.buckets, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}
