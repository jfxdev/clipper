// Package storetest holds a behavioral contract shared by every store.Store
// implementation, so redis/mongo/dynamo/memory are all held to the exact
// same semantics (expiry, burn-after-read atomicity, not-found handling).
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

// Run executes the full Store contract against s. It is safe to share a
// single Store instance across the whole suite: every sub-test uses its own
// randomly generated paste ID, so sub-tests never collide.
func Run(t *testing.T, s store.Store) {
	t.Helper()
	t.Run("CreateAndGet", func(t *testing.T) { testCreateAndGet(t, s) })
	t.Run("GetMissingReturnsNotFound", func(t *testing.T) { testGetMissing(t, s) })
	t.Run("ExpiredPasteIsNotFound", func(t *testing.T) { testExpired(t, s) })
	t.Run("ZeroExpiresAtNeverExpires", func(t *testing.T) { testNeverExpires(t, s) })
	t.Run("BurnAfterReadDeletesOnFirstGet", func(t *testing.T) { testBurnAfterRead(t, s) })
	t.Run("BurnAfterReadConcurrentGetsOnlyOneSucceeds", func(t *testing.T) { testBurnAfterReadConcurrent(t, s) })
}

func newID(t *testing.T) string {
	t.Helper()
	id, err := paste.NewID()
	if err != nil {
		t.Fatalf("paste.NewID() error = %v", err)
	}
	return id
}

func testCreateAndGet(t *testing.T, s store.Store) {
	ctx := context.Background()
	p := paste.Paste{
		ID:                newID(t),
		Data:              "hello world",
		PasswordProtected: true,
		CreatedAt:         time.Now().UTC().Truncate(time.Second),
		SizeBytes:         11,
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := s.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != p.ID || got.Data != p.Data || got.PasswordProtected != p.PasswordProtected {
		t.Fatalf("Get() = %+v, want fields matching %+v", got, p)
	}

	// Not burn-after-read: a second Get must still succeed.
	if _, err := s.Get(ctx, p.ID); err != nil {
		t.Fatalf("second Get() error = %v, want nil (paste is not burn-after-read)", err)
	}
}

func testGetMissing(t *testing.T, s store.Store) {
	ctx := context.Background()
	_, err := s.Get(ctx, newID(t))
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on missing id: err = %v, want store.ErrNotFound", err)
	}
}

func testExpired(t *testing.T, s store.Store) {
	ctx := context.Background()
	p := paste.Paste{
		ID:        newID(t),
		Data:      "will expire soon",
		ExpiresAt: time.Now().Add(150 * time.Millisecond),
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	if _, err := s.Get(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get() on expired paste: err = %v, want store.ErrNotFound", err)
	}
}

func testNeverExpires(t *testing.T, s store.Store) {
	ctx := context.Background()
	p := paste.Paste{
		ID:   newID(t),
		Data: "forever",
		// ExpiresAt left at zero value: never expires.
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := s.Get(ctx, p.ID); err != nil {
		t.Fatalf("Get() on never-expiring paste: err = %v, want nil", err)
	}
}

func testBurnAfterRead(t *testing.T, s store.Store) {
	ctx := context.Background()
	p := paste.Paste{
		ID:            newID(t),
		Data:          "read me once",
		BurnAfterRead: true,
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := s.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if got.Data != p.Data {
		t.Fatalf("first Get() Data = %q, want %q", got.Data, p.Data)
	}

	if _, err := s.Get(ctx, p.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second Get() on burned paste: err = %v, want store.ErrNotFound", err)
	}
}

func testBurnAfterReadConcurrent(t *testing.T, s store.Store) {
	ctx := context.Background()
	p := paste.Paste{
		ID:            newID(t),
		Data:          "only one reader wins",
		BurnAfterRead: true,
	}
	if err := s.Create(ctx, p); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const readers = 20
	var wg sync.WaitGroup
	var successes int32
	var mu sync.Mutex
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			if _, err := s.Get(ctx, p.ID); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if successes != 1 {
		t.Fatalf("concurrent Get() successes = %d, want exactly 1", successes)
	}
}
