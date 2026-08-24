package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jfxdev/clipper/backend/internal/store/memory"
)

// newModeRouter builds a router for a given MODE over a caller-supplied store,
// so a write to a "write" instance is visible when the same store is served
// by a "read" instance.
func newModeRouter(t *testing.T, s *memory.Store, mode string) http.Handler {
	t.Helper()
	q := NewQuota(QuotaConfig{MaxPastes: 1000, MaxBytes: 1 << 30, MaxClients: 100})
	t.Cleanup(q.Close)
	h := NewHandlers(s, HandlersConfig{
		MaxPasteSizeBytes: 1 << 20,
		MaxExpireSeconds:  30 * 24 * 60 * 60,
		Quota:             q,
	})
	rl := NewRateLimiter(RateLimiterConfig{
		RPS: 1e6, Burst: 1e6,
		GlobalRPS: 1e6, GlobalBurst: 1e6,
		MaxClients: 1000,
	})
	t.Cleanup(rl.Close)
	return NewRouter(h, rl, mode)
}

func TestModeReadRejectsCreateWithoutLeakingMode(t *testing.T) {
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	router := newModeRouter(t, s, "read")

	req := createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: testToken("frag"),
	}
	rec := doJSON(t, router, http.MethodPost, "/api/paste", req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create on read-only instance: status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	// The body must not name the mode or reveal read/write topology.
	if got := body["error"]; got != "forbidden" {
		t.Fatalf("error body = %q, want generic %q", got, "forbidden")
	}
}

func TestModeWriteRejectsGet(t *testing.T) {
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	router := newModeRouter(t, s, "write")

	rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/whatever", nil, testToken("frag"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get on write-only instance: status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestModeConfigEndpointReportsCapabilities(t *testing.T) {
	cases := map[string]struct{ create, read bool }{
		"":      {true, true},
		"read":  {false, true},
		"write": {true, false},
	}
	for mode, want := range cases {
		t.Run("mode="+mode, func(t *testing.T) {
			s := memory.New()
			t.Cleanup(func() { _ = s.Close(context.Background()) })
			router := newModeRouter(t, s, mode)

			rec := doJSON(t, router, http.MethodGet, "/api/config", nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("config status = %d, want 200", rec.Code)
			}
			var got clientConfigResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.CreateEnabled != want.create || got.ReadEnabled != want.read {
				t.Fatalf("config = %+v, want create=%v read=%v", got, want.create, want.read)
			}
		})
	}
}

// TestModeSplitSharedStore proves the intended topology: a write-only
// instance and a read-only instance over the same store cooperate — create on
// one, read on the other. Health stays reachable on both regardless of mode.
func TestModeSplitSharedStore(t *testing.T) {
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	writer := newModeRouter(t, s, "write")
	reader := newModeRouter(t, s, "read")

	frag := "shared-fragment"
	create := doJSON(t, writer, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: testToken(frag),
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create on write instance: status = %d body = %s", create.Code, create.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create body: %v", err)
	}

	get := doJSONWithToken(t, reader, http.MethodGet, "/api/paste/"+created.ID, nil, testToken(frag))
	if get.Code != http.StatusOK {
		t.Fatalf("get on read instance: status = %d body = %s", get.Code, get.Body.String())
	}

	for name, router := range map[string]http.Handler{"writer": writer, "reader": reader} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("health on %s instance: status = %d", name, rec.Code)
		}
	}
}
