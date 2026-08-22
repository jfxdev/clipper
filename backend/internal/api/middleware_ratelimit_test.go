package api

import (
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func testLimiter(rps float64, burst int) *RateLimiter {
	return NewRateLimiter(RateLimiterConfig{
		RPS: rps, Burst: burst,
		GlobalRPS: 1e6, GlobalBurst: 1e6,
		MaxClients: 1000,
	})
}

func TestRateLimiterAllowsBurstThenBlocksThenRefills(t *testing.T) {
	rl := testLimiter(10, 2)
	t.Cleanup(rl.Close)

	if !rl.allow("1.2.3.4") {
		t.Fatal("1st request should be allowed (within burst)")
	}
	if !rl.allow("1.2.3.4") {
		t.Fatal("2nd request should be allowed (within burst)")
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("3rd immediate request should be blocked (burst exhausted)")
	}

	time.Sleep(150 * time.Millisecond) // at 10 rps, ~1.5 tokens refill
	if !rl.allow("1.2.3.4") {
		t.Fatal("request after refill window should be allowed")
	}
}

func TestRateLimiterBucketsAreIndependentPerClient(t *testing.T) {
	rl := testLimiter(1, 1)
	t.Cleanup(rl.Close)

	if !rl.allow("1.1.1.1") {
		t.Fatal("first client's first request should be allowed")
	}
	if !rl.allow("2.2.2.2") {
		t.Fatal("second client should have its own independent bucket")
	}
}

// TestGlobalCeilingAppliesAcrossClients: per-client limits do nothing
// against a flood spread over many source addresses.
func TestGlobalCeilingAppliesAcrossClients(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RPS: 1000, Burst: 1000,
		GlobalRPS: 1, GlobalBurst: 2,
		MaxClients: 1000,
	})
	t.Cleanup(rl.Close)

	if !rl.allow("1.1.1.1") || !rl.allow("2.2.2.2") {
		t.Fatal("requests within the global burst should be allowed")
	}
	if rl.allow("3.3.3.3") {
		t.Fatal("a third distinct client should hit the global ceiling")
	}
}

// TestBucketMapIsBounded: without a cap, an attacker walking an address
// range allocates a limiter per address and exhausts memory.
func TestBucketMapIsBounded(t *testing.T) {
	rl := NewRateLimiter(RateLimiterConfig{
		RPS: 1000, Burst: 1000,
		GlobalRPS: 1e6, GlobalBurst: 1e6,
		MaxClients: 4,
	})
	t.Cleanup(rl.Close)

	for i := 0; i < 100; i++ {
		rl.allow(netip.AddrFrom4([4]byte{10, 0, byte(i / 256), byte(i % 256)}).String())
	}
	rl.mu.Lock()
	size := len(rl.buckets)
	rl.mu.Unlock()
	if size > 4 {
		t.Fatalf("bucket map grew to %d entries, want at most 4", size)
	}
}

// TestIPv6ClientsBucketByPrefix: a single subscriber is routinely handed a
// whole /64, so per-address buckets would make the limit meaningless.
func TestIPv6ClientsBucketByPrefix(t *testing.T) {
	a := clientKey(netip.MustParseAddr("2001:db8:1:2::1"))
	b := clientKey(netip.MustParseAddr("2001:db8:1:2:ffff:ffff:ffff:ffff"))
	if a != b {
		t.Fatalf("addresses in the same /64 keyed differently: %q vs %q", a, b)
	}
	c := clientKey(netip.MustParseAddr("2001:db8:1:3::1"))
	if a == c {
		t.Fatalf("addresses in different /64s share a key: %q", a)
	}
	if got := clientKey(netip.MustParseAddr("203.0.113.5")); got != "203.0.113.5" {
		t.Fatalf("IPv4 key = %q, want the exact address", got)
	}
}

func newForwardedRequest(remote, xff string) *http.Request {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	return req
}

func TestClientIPIgnoresForwardedHeaderByDefault(t *testing.T) {
	req := newForwardedRequest("10.0.0.1:5555", "203.0.113.9, 198.51.100.7")
	if got := clientIP(req, false, nil); got != "10.0.0.1" {
		t.Fatalf("clientIP() = %q, want %q (RemoteAddr, header ignored)", got, "10.0.0.1")
	}
}

// TestClientIPWithTrustedProxy walks X-Forwarded-For from the right,
// skipping hops we vouched for, and stops at the first address we did not:
// entries to the left of that are attacker-controlled.
func TestClientIPWithTrustedProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	req := newForwardedRequest("10.0.0.1:5555", "203.0.113.9, 198.51.100.7, 10.0.0.2")
	if got := clientIP(req, false, trusted); got != "198.51.100.7" {
		t.Fatalf("clientIP() = %q, want %q (first untrusted hop from the right)", got, "198.51.100.7")
	}

	// A peer outside the trusted set gets no say at all, however convincing
	// its header looks.
	untrusted := newForwardedRequest("203.0.113.50:5555", "1.1.1.1")
	if got := clientIP(untrusted, false, trusted); got != "203.0.113.50" {
		t.Fatalf("clientIP() from untrusted peer = %q, want the peer address", got)
	}
}

func TestClientIPTrustProxyShorthand(t *testing.T) {
	req := newForwardedRequest("10.0.0.1:5555", "203.0.113.9")
	if got := clientIP(req, true, nil); got != "203.0.113.9" {
		t.Fatalf("clientIP(trustProxy=true) = %q, want %q", got, "203.0.113.9")
	}
}

// TestClientIPSpoofedHeaderCannotRotateBuckets is the whole point of the
// right-to-left walk: a client that prepends junk must still land in the
// same bucket every time.
func TestClientIPSpoofedHeaderCannotRotateBuckets(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	first := clientIP(newForwardedRequest("10.0.0.1:1", "9.9.9.9, 198.51.100.7, 10.0.0.2"), false, trusted)
	second := clientIP(newForwardedRequest("10.0.0.1:2", "8.8.8.8, 198.51.100.7, 10.0.0.2"), false, trusted)
	if first != second {
		t.Fatalf("spoofed left-hand entries changed the bucket: %q vs %q", first, second)
	}
}
