package api

import (
	"net/netip"
	"testing"
	"time"
)

func testQuota(t *testing.T, maxPastes int, maxBytes int64, maxClients int) *Quota {
	t.Helper()
	q := NewQuota(QuotaConfig{MaxPastes: maxPastes, MaxBytes: maxBytes, MaxClients: maxClients})
	t.Cleanup(q.Close)
	return q
}

func TestQuotaCountsPastesAndBytes(t *testing.T) {
	q := testQuota(t, 3, 100, 10)
	now := time.Now()

	for i := 0; i < 3; i++ {
		if !q.charge("client", 10, now) {
			t.Fatalf("charge %d rejected within allowance", i)
		}
	}
	if q.charge("client", 10, now) {
		t.Fatal("charge past MaxPastes was allowed")
	}

	byteBound := testQuota(t, 100, 25, 10)
	if !byteBound.charge("client", 20, now) {
		t.Fatal("first charge rejected")
	}
	if byteBound.charge("client", 20, now) {
		t.Fatal("charge past MaxBytes was allowed")
	}
}

func TestQuotaWindowRollsOver(t *testing.T) {
	q := testQuota(t, 1, 100, 10)
	start := time.Now()
	if !q.charge("client", 10, start) {
		t.Fatal("first charge rejected")
	}
	if q.charge("client", 10, start) {
		t.Fatal("second charge inside the window was allowed")
	}
	if !q.charge("client", 10, start.Add(quotaWindow+time.Second)) {
		t.Fatal("charge after the window rolled over was rejected")
	}
}

// TestQuotaRefundRestoresAllowance: a datastore failure must not spend the
// client's allowance for a paste that was never stored.
func TestQuotaRefundRestoresAllowance(t *testing.T) {
	q := testQuota(t, 1, 100, 10)
	now := time.Now()

	if !q.charge("client", 40, now) {
		t.Fatal("first charge rejected")
	}
	q.refund("client", 40, now)
	if !q.charge("client", 40, now) {
		t.Fatal("charge after refund rejected: the refund did not restore the allowance")
	}
}

// TestQuotaRefundCannotGoNegative guards against a refund being used to
// mint allowance — an unmatched or repeated refund must not push the
// counters below zero.
func TestQuotaRefundCannotGoNegative(t *testing.T) {
	q := testQuota(t, 5, 100, 10)
	now := time.Now()

	if !q.charge("client", 10, now) {
		t.Fatal("charge rejected")
	}
	for i := 0; i < 5; i++ {
		q.refund("client", 10, now)
	}

	q.mu.Lock()
	u := q.usage["client"]
	pastes, bytes := u.pastes, u.bytes
	q.mu.Unlock()
	if pastes < 0 || bytes < 0 {
		t.Fatalf("counters went negative: pastes=%d bytes=%d", pastes, bytes)
	}

	// Refunding an unknown client must be a no-op, not a panic.
	q.refund("never-seen", 10, now)
}

// TestQuotaStaysAvailableWhenMapIsFull is the availability property: an
// attacker rotating source addresses can fill the tracking map, and that
// must not stop legitimate clients from creating pastes. Refusing every
// untracked client here would turn an accounting cap into a global outage
// lasting a whole quota window.
func TestQuotaStaysAvailableWhenMapIsFull(t *testing.T) {
	q := testQuota(t, 100, 1<<30, 16)
	now := time.Now()

	// Flood the map from many distinct /64s.
	for i := 0; i < 500; i++ {
		addr := netip.AddrFrom16([16]byte{0x20, 0x01, 0xd, 0xb8, byte(i >> 8), byte(i), 0, 1})
		if !q.charge(clientKey(addr), 1, now) {
			t.Fatalf("flood charge %d rejected; the map is failing closed", i)
		}
	}

	if !q.charge("legitimate-client", 1, now) {
		t.Fatal("a new legitimate client was denied after the map filled up")
	}

	q.mu.Lock()
	size := len(q.usage)
	q.mu.Unlock()
	if size > 16 {
		t.Fatalf("usage map grew to %d entries, want at most 16", size)
	}
}
