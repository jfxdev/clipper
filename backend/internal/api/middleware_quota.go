package api

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// quotaWindow is the rolling period a client's storage allowance covers.
const quotaWindow = 24 * time.Hour

// QuotaConfig configures NewQuota.
type QuotaConfig struct {
	// MaxPastes and MaxBytes are what one client may store per window.
	MaxPastes int
	MaxBytes  int64

	// MaxClients bounds the tracking map for the same reason the rate
	// limiter bounds its own: the keys are unauthenticated and attacker
	// chosen.
	MaxClients int

	TrustProxy     bool
	TrustedProxies []netip.Prefix
}

type usage struct {
	pastes      int
	bytes       int64
	windowStart time.Time
}

// Quota limits how much storage a single client can consume per day.
//
// Request-rate limiting alone does not bound this: the default 5 rps
// sustained for an hour is ~18k pastes, which at the 2 MB ceiling is tens
// of gigabytes. Storage exhaustion is the cheapest denial-of-service
// against a service whose whole job is to hold data for other people.
type Quota struct {
	mu    sync.Mutex
	usage map[string]*usage

	maxPastes  int
	maxBytes   int64
	maxClients int

	trustProxy     bool
	trustedProxies []netip.Prefix

	stop chan struct{}
	once sync.Once
}

func NewQuota(cfg QuotaConfig) *Quota {
	q := &Quota{
		usage:          make(map[string]*usage),
		maxPastes:      cfg.MaxPastes,
		maxBytes:       cfg.MaxBytes,
		maxClients:     cfg.MaxClients,
		trustProxy:     cfg.TrustProxy,
		trustedProxies: cfg.TrustedProxies,
		stop:           make(chan struct{}),
	}
	go q.evictLoop()
	return q
}

// Close stops the background eviction goroutine. Safe to call more than once.
func (q *Quota) Close() {
	q.once.Do(func() { close(q.stop) })
}

// Charge records size bytes against the request's client and reports
// whether the client is still within its allowance. It is called after the
// body has been read and validated, so a rejected request costs nothing.
func (q *Quota) Charge(r *http.Request, size int64) bool {
	return q.charge(clientIP(r, q.trustProxy, q.trustedProxies), size, time.Now())
}

func (q *Quota) charge(key string, size int64, now time.Time) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	u, ok := q.usage[key]
	if !ok {
		if len(q.usage) >= q.maxClients {
			// The map is full of other clients' counters. Refusing here is
			// the conservative choice: the alternative is letting the cap
			// be bypassed by whoever arrives once the map fills up.
			return false
		}
		u = &usage{windowStart: now}
		q.usage[key] = u
	}
	if now.Sub(u.windowStart) >= quotaWindow {
		u.pastes, u.bytes, u.windowStart = 0, 0, now
	}
	if u.pastes+1 > q.maxPastes || u.bytes+size > q.maxBytes {
		return false
	}
	u.pastes++
	u.bytes += size
	return true
}

func (q *Quota) evictLoop() {
	ticker := time.NewTicker(quotaWindow / 24)
	defer ticker.Stop()
	for {
		select {
		case <-q.stop:
			return
		case <-ticker.C:
			now := time.Now()
			q.mu.Lock()
			for key, u := range q.usage {
				if now.Sub(u.windowStart) >= quotaWindow {
					delete(q.usage, key)
				}
			}
			q.mu.Unlock()
		}
	}
}
