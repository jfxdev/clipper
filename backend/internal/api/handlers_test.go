package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfxdev/clipper/backend/internal/store/memory"
)

func newTestRouter(t *testing.T, maxSize int64, rps float64, burst int) http.Handler {
	t.Helper()
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	h := NewHandlers(s, maxSize)
	rl := NewRateLimiter(rps, burst, false)
	t.Cleanup(rl.Close)
	return NewRouter(h, rl)
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestCreateAndGetPaste(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)

	createRec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data:              "ciphertext-blob",
		PasswordProtected: true,
	})
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}
	var created createPasteResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create response has empty id")
	}

	getRec := doJSON(t, router, http.MethodGet, "/api/paste/"+created.ID, nil)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var got getPasteResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Data != "ciphertext-blob" || !got.PasswordProtected {
		t.Fatalf("get response = %+v, unexpected fields", got)
	}
}

func TestGetPasteNotFound(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodGet, "/api/paste/does-not-exist", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBurnAfterReadOnlyReadableOnce(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)

	createRec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data:          "burn-me",
		BurnAfterRead: true,
	})
	var created createPasteResponse
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)

	first := doJSON(t, router, http.MethodGet, "/api/paste/"+created.ID, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first get status = %d", first.Code)
	}
	second := doJSON(t, router, http.MethodGet, "/api/paste/"+created.ID, nil)
	if second.Code != http.StatusNotFound {
		t.Fatalf("second get status = %d, want 404", second.Code)
	}
}

func TestCreatePasteEmptyDataRejected(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: ""})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreatePasteTooLargeRejected(t *testing.T) {
	router := newTestRouter(t, 10, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: strings.Repeat("a", 11)})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

// TestCreatePasteExceedsMaxBytesReaderRejected covers a request whose raw
// body already exceeds maxSize+requestOverhead, so http.MaxBytesReader
// aborts the JSON decode itself rather than paste.Validate ever running.
// This path must still map to 413, not a generic 400.
func TestCreatePasteExceedsMaxBytesReaderRejected(t *testing.T) {
	router := newTestRouter(t, 10, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: strings.Repeat("a", 5000)})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePasteNegativeExpireRejected(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: "x", ExpireSeconds: -1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreatePasteExpireSecondsAtMaxAccepted(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: "x", ExpireSeconds: maxExpireSeconds})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePasteExpireSecondsAboveMaxRejected(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: "x", ExpireSeconds: maxExpireSeconds + 1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreatePasteRejectsUnknownFields(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", map[string]any{
		"data":       "x",
		"unexpected": "field",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePasteRejectsTrailingContent(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/paste",
		strings.NewReader(`{"data":"x"}{"data":"y"}`))
	req.RemoteAddr = "203.0.113.1:12345"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestRateLimitReturns429(t *testing.T) {
	router := newTestRouter(t, 1024, 1, 1) // 1 rps, burst 1: second immediate request is limited

	first := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: "a"})
	if first.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want 201", first.Code)
	}
	second := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{Data: "b"})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", second.Code)
	}
}

func TestHealth(t *testing.T) {
	router := newTestRouter(t, 1024, 100, 100)
	rec := doJSON(t, router, http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
