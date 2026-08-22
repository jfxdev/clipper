package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func securedResponse(t *testing.T, cfg SecurityHeadersConfig) *httptest.ResponseRecorder {
	t.Helper()
	h := SecurityHeaders(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/paste/abc", nil))
	return rec
}

// TestSecurityHeadersPresent is a regression net around the controls that
// carry clipper's confidentiality claim. An XSS here does not deface a
// page, it hands over the decryption key sitting in location.hash.
func TestSecurityHeadersPresent(t *testing.T) {
	rec := securedResponse(t, SecurityHeadersConfig{})

	want := map[string]string{
		"X-Content-Type-Options":       "nosniff",
		"X-Frame-Options":              "DENY",
		"Referrer-Policy":              "no-referrer",
		"Cross-Origin-Opener-Policy":   "same-origin",
		"Cross-Origin-Resource-Policy": "same-origin",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}

	csp := rec.Header().Get("Content-Security-Policy")
	for _, directive := range []string{
		"default-src 'none'",
		"script-src 'self'",
		"connect-src 'self'",
		"base-uri 'none'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP is missing %q: %s", directive, csp)
		}
	}
	if strings.Contains(csp, "script-src 'self' 'unsafe-inline'") || strings.Contains(csp, "unsafe-eval") {
		t.Errorf("CSP allows inline or eval'd script: %s", csp)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store by default", cc)
	}
}

func TestHSTSOnlyWhenConfigured(t *testing.T) {
	if got := securedResponse(t, SecurityHeadersConfig{}).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q, want empty when unconfigured", got)
	}
	got := securedResponse(t, SecurityHeadersConfig{HSTSMaxAgeSeconds: 31536000}).Header().Get("Strict-Transport-Security")
	if !strings.Contains(got, "max-age=31536000") {
		t.Errorf("Strict-Transport-Security = %q, want max-age=31536000", got)
	}
}

func TestRequireSameOrigin(t *testing.T) {
	handler := RequireSameOrigin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name       string
		fetchSite  string
		origin     string
		host       string
		wantStatus int
	}{
		{"no browser headers", "", "", "clipper.example", http.StatusNoContent},
		{"same origin", "same-origin", "https://clipper.example", "clipper.example", http.StatusNoContent},
		{"direct navigation", "none", "", "clipper.example", http.StatusNoContent},
		{"cross site", "cross-site", "https://evil.example", "clipper.example", http.StatusForbidden},
		{"same site subdomain", "same-site", "https://other.example", "clipper.example", http.StatusForbidden},
		{"mismatched origin only", "", "https://evil.example", "clipper.example", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/paste", nil)
			req.Host = tc.host
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestRecoverReturns500 keeps a panic from truncating a response and
// leaking a stack trace to the caller.
func TestRecoverReturns500(t *testing.T) {
	handler := Recover(errorLogger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/health", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("panic value leaked into the response body: %s", rec.Body.String())
	}
}

// TestRedactPathKeepsPasteIDsOutOfLogs: the ID identifies a specific
// secret, and log pipelines are exactly where it should not accumulate.
func TestRedactPathKeepsPasteIDsOutOfLogs(t *testing.T) {
	cases := map[string]string{
		"/api/paste/AAAAAAAAAAAAAAAAAAAAAA": "/api/paste/{id}",
		"/paste/AAAAAAAAAAAAAAAAAAAAAA":     "/paste/{id}",
		"/api/health":                       "/api/health",
		"/":                                 "/",
	}
	for path, want := range cases {
		if got := redactPath(path); got != want {
			t.Errorf("redactPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestSanitizeForLogStopsForgedEntries: a request path is attacker-chosen,
// so anything logged from one must not be able to carry a newline (which
// forges a log line) or a terminal escape, and must not be unbounded.
func TestSanitizeForLogStopsForgedEntries(t *testing.T) {
	forged := "/evil\n2026-01-01 INFO admin login succeeded\r\n"
	got := sanitizeForLog(forged)
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("sanitizeForLog kept control character %q in %q", r, got)
		}
	}

	if got := sanitizeForLog("/\x1b[2J\x1b[H"); strings.ContainsRune(got, 0x1b) {
		t.Fatalf("sanitizeForLog kept an escape character: %q", got)
	}

	long := sanitizeForLog("/" + strings.Repeat("a", 10_000))
	if len([]rune(long)) > maxLoggedPathLen+1 {
		t.Fatalf("sanitizeForLog returned %d runes, want the path bounded", len([]rune(long)))
	}

	// Legitimate paths, including non-ASCII ones, pass through unchanged.
	for _, ok := range []string{"/api/health", "/paste", "/segredo-ção"} {
		if got := sanitizeForLog(ok); got != ok {
			t.Errorf("sanitizeForLog(%q) = %q, want it unchanged", ok, got)
		}
	}
}

// TestRedactPathSanitizesUnknownPaths: paths that are not a known route
// still reach the log, so they go through the same sanitizer.
func TestRedactPathSanitizesUnknownPaths(t *testing.T) {
	if got := redactPath("/unknown\ninjected"); strings.ContainsRune(got, '\n') {
		t.Fatalf("redactPath left a newline in an unknown path: %q", got)
	}
}
