package api

import (
	"net/http"
	"testing"
	"time"
)

func TestRateLimiterAllowsBurstThenBlocksThenRefills(t *testing.T) {
	rl := NewRateLimiter(10, 2, false) // 10 rps, burst 2
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

func TestRateLimiterBucketsAreIndependentPerIP(t *testing.T) {
	rl := NewRateLimiter(1, 1, false)
	t.Cleanup(rl.Close)

	if !rl.allow("1.1.1.1") {
		t.Fatal("first IP's first request should be allowed")
	}
	if !rl.allow("2.2.2.2") {
		t.Fatal("second IP should have its own independent bucket")
	}
}

func TestClientIPTrustsProxyOnlyWhenConfigured(t *testing.T) {
	// X-Forwarded-For is "client, hop1, hop2, ..." — each proxy appends what
	// it saw. With a single trusted reverse proxy in front of us, the
	// rightmost entry is the one *it* appended, and is what we should
	// trust; the leftmost entry is client-supplied and spoofable.
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")

	if got := clientIP(req, false); got != "10.0.0.1" {
		t.Fatalf("clientIP(trustProxy=false) = %q, want %q (RemoteAddr, header ignored)", got, "10.0.0.1")
	}
	if got := clientIP(req, true); got != "198.51.100.7" {
		t.Fatalf("clientIP(trustProxy=true) = %q, want %q (rightmost/trusted-proxy-appended entry)", got, "198.51.100.7")
	}
}

func TestClientIPSingleForwardedForEntry(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")

	if got := clientIP(req, true); got != "203.0.113.9" {
		t.Fatalf("clientIP(trustProxy=true) = %q, want %q", got, "203.0.113.9")
	}
}
