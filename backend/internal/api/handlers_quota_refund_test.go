package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/jfxdev/clipper/backend/internal/paste"
)

// failingStore fails every Create with a caller-supplied error, so the
// handler's refund decision can be exercised without a real datastore.
type failingStore struct{ err error }

func (s failingStore) Create(context.Context, paste.Paste) error { return s.err }
func (s failingStore) Get(context.Context, string, string) (paste.Paste, error) {
	return paste.Paste{}, errors.New("unused")
}
func (s failingStore) Close(context.Context) error { return nil }

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

// TestQuotaRefundOnlyForDefiniteFailures is the accounting rule around an
// unreliable store: a write that definitely did not land is refunded, and a
// write whose outcome is unknown is not. Refunding the unknown case would
// let a client retry its way past the quota — every attempt charged and
// refunded while pastes pile up on the far side.
func TestQuotaRefundOnlyForDefiniteFailures(t *testing.T) {
	cases := []struct {
		name       string
		storeErr   error
		wantRefund bool
	}{
		{"connection refused is a definite no-write", errors.New("dial tcp: connection refused"), true},
		{"deadline exceeded is ambiguous", context.DeadlineExceeded, false},
		{"cancellation is ambiguous", context.Canceled, false},
		{"wrapped deadline is ambiguous", errors.Join(errors.New("mongo: write failed"), context.DeadlineExceeded), false},
		{"network timeout is ambiguous", timeoutError{}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := NewQuota(QuotaConfig{MaxPastes: 1, MaxBytes: 1 << 20, MaxClients: 10})
			t.Cleanup(q.Close)
			h := NewHandlers(failingStore{err: tc.storeErr}, HandlersConfig{
				MaxPasteSizeBytes: 4096,
				MaxExpireSeconds:  3600,
				Quota:             q,
			})
			rl := NewRateLimiter(RateLimiterConfig{RPS: 1000, Burst: 1000, GlobalRPS: 1e6, GlobalBurst: 1e6, MaxClients: 10})
			t.Cleanup(rl.Close)
			router := NewRouter(h, rl)

			body := createPasteRequest{
				Data: testEnvelope(30), ExpireSeconds: 60, ReadToken: testToken("f"),
			}
			if rec := doJSON(t, router, http.MethodPost, "/api/paste", body); rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", rec.Code)
			}

			// MaxPastes is 1, so whether the allowance came back is
			// observable as whether a second attempt is charged at all.
			charged := q.charge("203.0.113.1", 1, time.Now())
			if charged != tc.wantRefund {
				t.Fatalf("allowance available after failure = %v, want %v", charged, tc.wantRefund)
			}
		})
	}
}

func TestWriteOutcomeIsUnknown(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !writeOutcomeIsUnknown(cancelled, errors.New("whatever")) {
		t.Error("a cancelled request context should mark the outcome unknown")
	}
	if writeOutcomeIsUnknown(context.Background(), errors.New("connection refused")) {
		t.Error("a plain error on a healthy context should be a definite failure")
	}
}
