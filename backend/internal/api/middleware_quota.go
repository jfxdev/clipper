package api

import (
	"net/http"
	"net/netip"
	"sync"
	"time"
)

// quotaWindow is the rolling period a client's storage allowance covers.
const quotaWindow = 24 * time.Hour

// evictionSampleSize is how many tracked clients are examined when the map
// is full and room has to be made. Scanning the whole map would turn every
// request past the cap into an O(maxClients) walk under the lock — a
// cheaper denial of service than the one the cap exists to prevent. Taking
// the oldest of a small sample (Go randomizes map iteration order) is the
// same approximate-LRU trade-off Redis makes, at constant cost.
const evictionSampleSize = 8

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
			// Make room rather than refuse. Refusing every untracked client
			// once the map fills would let an attacker rotating source
			// addresses lock the whole service out of paste creation for a
			// full quota window — turning an accounting cap into a global
			// outage. Evicting instead degrades accuracy (an attacker who
			// can fill the map can also cycle their own counter out of it)
			// while keeping the service available; the rate limiters are
			// what bound that attacker's throughput.
			q.evictOldestSample(now)
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

// Refund returns a previously charged allowance to the request's client.
// It is called when the work the charge paid for did not happen — a failed
// datastore write, say — so an outage does not quietly consume the
// allowance of every client that hit it.
func (q *Quota) Refund(r *http.Request, size int64) {
	q.refund(clientIP(r, q.trustProxy, q.trustedProxies), size, time.Now())
}

func (q *Quota) refund(key string, size int64, now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()

	u, ok := q.usage[key]
	if !ok {
		return
	}
	// If the window rolled over between the charge and the refund, the
	// counters no longer include this charge and must not go negative.
	if now.Sub(u.windowStart) >= quotaWindow {
		return
	}
	if u.pastes > 0 {
		u.pastes--
	}
	u.bytes -= size
	if u.bytes < 0 {
		u.bytes = 0
	}
}

// evictOldestSample removes the least recently started window among a small
// random sample of tracked clients. The caller must hold q.mu.
func (q *Quota) evictOldestSample(now time.Time) {
	var (
		oldestKey string
		oldest    time.Time
		seen      int
	)
	for key, u := range q.usage {
		// An entry whose window has already rolled over is dead weight;
		// take it immediately rather than sampling further.
		if now.Sub(u.windowStart) >= quotaWindow {
			delete(q.usage, key)
			return
		}
		if oldestKey == "" || u.windowStart.Before(oldest) {
			oldestKey, oldest = key, u.windowStart
		}
		if seen++; seen >= evictionSampleSize {
			break
		}
	}
	if oldestKey != "" {
		delete(q.usage, oldestKey)
	}
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
