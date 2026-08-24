package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfxdev/clipper/backend/internal/store/memory"
)

const testExpireSeconds = 3600

// testEnvelope builds a structurally valid encrypted envelope of roughly
// the requested ciphertext length. paste.Validate inspects the framing, so
// tests can no longer post arbitrary strings as data.
func testEnvelope(ctLen int) string {
	if ctLen < 22 {
		ctLen = 22
	}
	return fmt.Sprintf(`{"v":1,"iter":0,"iv":"%s","ct":"%s"}`,
		strings.Repeat("A", 16), strings.Repeat("A", ctLen))
}

// testToken mirrors what the frontend derives from the key fragment.
func testToken(fragment string) string {
	sum := sha256.Sum256([]byte(fragment))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func newTestRouter(t *testing.T, maxSize int64, rps float64, burst int) http.Handler {
	t.Helper()
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	q := NewQuota(QuotaConfig{MaxPastes: 1000, MaxBytes: 1 << 30, MaxClients: 100})
	t.Cleanup(q.Close)
	h := NewHandlers(s, HandlersConfig{
		MaxPasteSizeBytes: maxSize,
		MaxExpireSeconds:  30 * 24 * 60 * 60,
		Quota:             q,
	})
	rl := NewRateLimiter(RateLimiterConfig{
		RPS: rps, Burst: burst,
		GlobalRPS: 1e6, GlobalBurst: 1e6,
		MaxClients: 1000,
	})
	t.Cleanup(rl.Close)
	return NewRouter(h, rl, "")
}

func doJSON(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONWithToken(t, router, method, path, body, "")
}

func doJSONWithToken(t *testing.T, router http.Handler, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.RemoteAddr = "203.0.113.1:12345"
	if token != "" {
		req.Header.Set(readTokenHeader, token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// createForTest posts a valid paste and returns its id.
func createForTest(t *testing.T, router http.Handler, req createPasteRequest) string {
	t.Helper()
	rec := doJSON(t, router, http.MethodPost, "/api/paste", req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var created createPasteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create response has empty id")
	}
	return created.ID
}

func TestCreateAndGetPaste(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	token := testToken("fragment-a")
	data := testEnvelope(40)

	id := createForTest(t, router, createPasteRequest{
		Data:              data,
		ExpireSeconds:     testExpireSeconds,
		PasswordProtected: true,
		ReadToken:         token,
	})

	getRec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, token)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var got getPasteResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got.Data != data || !got.PasswordProtected {
		t.Fatalf("get response = %+v, unexpected fields", got)
	}
}

// TestGetWithoutReadTokenIsNotFound is the property that makes a paste ID
// safe to have in a proxy log: on its own it grants nothing.
func TestGetWithoutReadTokenIsNotFound(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	id := createForTest(t, router, createPasteRequest{
		Data: testEnvelope(40), ExpireSeconds: testExpireSeconds, ReadToken: testToken("frag"),
	})

	if rec := doJSON(t, router, http.MethodGet, "/api/paste/"+id, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("get without token status = %d, want 404", rec.Code)
	}
	if rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, testToken("wrong")); rec.Code != http.StatusNotFound {
		t.Fatalf("get with wrong token status = %d, want 404", rec.Code)
	}
}

// TestWrongTokenCannotBurn covers the destructive half: someone who knows
// the ID must not be able to destroy a burn-after-read message.
func TestWrongTokenCannotBurn(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	token := testToken("frag")
	id := createForTest(t, router, createPasteRequest{
		Data: testEnvelope(40), ExpireSeconds: testExpireSeconds, BurnAfterRead: true, ReadToken: token,
	})

	for i := 0; i < 3; i++ {
		if rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, testToken("attacker")); rec.Code != http.StatusNotFound {
			t.Fatalf("attacker get status = %d, want 404", rec.Code)
		}
	}
	if rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, token); rec.Code != http.StatusOK {
		t.Fatalf("legitimate get after failed attempts = %d, want 200", rec.Code)
	}
}

func TestGetPasteNotFound(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/AAAAAAAAAAAAAAAAAAAAAA", nil, testToken("x"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestMalformedIDLooksLikeAMiss keeps a scanner from distinguishing "not a
// real ID shape" from "valid shape, no such paste".
func TestMalformedIDLooksLikeAMiss(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	for _, id := range []string{"short", "has/slash", strings.Repeat("A", 300), "**********************"} {
		rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, testToken("x"))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("get %q status = %d, want 404", id, rec.Code)
		}
	}
}

func TestBurnAfterReadOnlyReadableOnce(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	token := testToken("frag")
	id := createForTest(t, router, createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, BurnAfterRead: true, ReadToken: token,
	})

	if first := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, token); first.Code != http.StatusOK {
		t.Fatalf("first get status = %d", first.Code)
	}
	if second := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, token); second.Code != http.StatusNotFound {
		t.Fatalf("second get status = %d, want 404", second.Code)
	}
}

func TestCreatePasteEmptyDataRejected(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: "", ExpireSeconds: testExpireSeconds, ReadToken: testToken("f"),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestCreatePasteRejectsNonEnvelopeData stops the service being used as
// anonymous hosting for arbitrary bytes: the payload must at least be
// shaped like something this app's client produced.
func TestCreatePasteRejectsNonEnvelopeData(t *testing.T) {
	router := newTestRouter(t, 65536, 100, 100)
	cases := map[string]string{
		"plain text":       "just some text",
		"wrong version":    `{"v":2,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`,
		"bad iv length":    `{"v":1,"iter":0,"iv":"AAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`,
		"non base64 ct":    `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"!!!!!!!!!!!!!!!!!!!!!!"}`,
		"huge iterations":  `{"v":1,"iter":999999999,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"}`,
		"extra field":      `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA","x":1}`,
		"trailing content": `{"v":1,"iter":0,"iv":"AAAAAAAAAAAAAAAA","ct":"AAAAAAAAAAAAAAAAAAAAAA"} trailing`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
				Data: data, ExpireSeconds: testExpireSeconds, ReadToken: testToken("f"),
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreatePasteRejectsMissingOrMalformedReadToken(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	for name, token := range map[string]string{
		"empty":     "",
		"too short": "abc",
		"bad chars": strings.Repeat("*", 43),
	} {
		t.Run(name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
				Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: token,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestCreatePasteTooLargeRejected(t *testing.T) {
	router := newTestRouter(t, 64, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(200), ExpireSeconds: testExpireSeconds, ReadToken: testToken("f"),
	})
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
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(5000), ExpireSeconds: testExpireSeconds, ReadToken: testToken("f"),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", rec.Code, rec.Body.String())
	}
}

// TestCreatePasteRequiresExpiry: a secret that never expires outlives the
// reason it was shared, so "no expiry" is not an option the API offers.
func TestCreatePasteRequiresExpiry(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	for _, expire := range []int64{0, -1} {
		rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
			Data: testEnvelope(30), ExpireSeconds: expire, ReadToken: testToken("f"),
		})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expireSeconds=%d status = %d, want 400", expire, rec.Code)
		}
	}
}

func TestCreatePasteExpireSecondsAboveMaxRejected(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: 30*24*60*60 + 1, ReadToken: testToken("f"),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreatePasteRejectsUnknownFields(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSON(t, router, http.MethodPost, "/api/paste", map[string]any{
		"data":       testEnvelope(30),
		"unexpected": "field",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreatePasteRejectsTrailingContent(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
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
	router := newTestRouter(t, 4096, 1, 1) // 1 rps, burst 1: second immediate request is limited
	first := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: testToken("a"),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("first request status = %d, want 201", first.Code)
	}
	second := doJSON(t, router, http.MethodPost, "/api/paste", createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: testToken("b"),
	})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want 429", second.Code)
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("429 response is missing Retry-After")
	}
}

// TestQuotaBlocksStorageFlood: request-rate limits alone do not bound how
// much a client can store.
func TestQuotaBlocksStorageFlood(t *testing.T) {
	s := memory.New()
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	q := NewQuota(QuotaConfig{MaxPastes: 2, MaxBytes: 1 << 30, MaxClients: 10})
	t.Cleanup(q.Close)
	h := NewHandlers(s, HandlersConfig{MaxPasteSizeBytes: 4096, MaxExpireSeconds: 3600, Quota: q})
	rl := NewRateLimiter(RateLimiterConfig{RPS: 1000, Burst: 1000, GlobalRPS: 1e6, GlobalBurst: 1e6, MaxClients: 10})
	t.Cleanup(rl.Close)
	router := NewRouter(h, rl, "")

	body := createPasteRequest{Data: testEnvelope(30), ExpireSeconds: 60, ReadToken: testToken("f")}
	for i := 0; i < 2; i++ {
		if rec := doJSON(t, router, http.MethodPost, "/api/paste", body); rec.Code != http.StatusCreated {
			t.Fatalf("paste %d status = %d, want 201", i, rec.Code)
		}
	}
	if rec := doJSON(t, router, http.MethodPost, "/api/paste", body); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-quota status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
}

// TestCrossOriginCreateRejected: another site must not be able to drive
// paste creation through a visitor's browser.
func TestCrossOriginCreateRejected(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: testToken("f"),
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/paste", &buf)
	req.RemoteAddr = "203.0.113.1:12345"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSON(t, router, http.MethodGet, "/api/nope", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want JSON", ct)
	}
}

// TestAPIResponsesAreNotCacheable: paste ciphertext must never be stored by
// a shared proxy or the browser's disk cache.
func TestAPIResponsesAreNotCacheable(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	token := testToken("frag")
	id := createForTest(t, router, createPasteRequest{
		Data: testEnvelope(30), ExpireSeconds: testExpireSeconds, ReadToken: token,
	})
	rec := doJSONWithToken(t, router, http.MethodGet, "/api/paste/"+id, nil, token)
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}

func TestHealth(t *testing.T) {
	router := newTestRouter(t, 4096, 100, 100)
	rec := doJSON(t, router, http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
