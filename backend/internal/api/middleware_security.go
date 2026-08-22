package api

import (
	"fmt"
	"net/http"
	"strings"
)

// contentSecurityPolicy is the single most important control in this
// service. clipper's whole confidentiality claim rests on the decryption
// key never leaving the browser — but the key sits in location.hash and the
// plaintext sits in the DOM, so any injected script is a total compromise,
// not a defacement. The policy is therefore deny-by-default and allows
// nothing to be fetched from, or sent to, anywhere but this origin.
//
//   - script-src 'self': no inline scripts at all. The Vite build is
//     configured (frontend/vite.config.ts) to keep its module-preload
//     polyfill out of index.html so no hash/nonce exception is needed.
//   - style-src allows 'unsafe-inline' because Radix/shadcn set inline
//     style attributes for positioning. CSS injection cannot exfiltrate
//     anything here: every fetch directive is locked to 'self', so there is
//     no url() an attacker could point at their own host.
//   - form-action 'none' and base-uri 'none' close the two classic ways of
//     smuggling data out without script execution.
//   - frame-ancestors 'none' blocks clickjacking, which for this app means
//     framing a paste link and stealing the click that confirms a burn.
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// SecurityHeadersConfig controls the parts of the header set that depend on
// how the service is deployed.
type SecurityHeadersConfig struct {
	// HSTSMaxAgeSeconds emits Strict-Transport-Security when > 0. It is off
	// by default: sending HSTS from a plain-HTTP listener that is reachable
	// directly would lock clients out.
	HSTSMaxAgeSeconds int
}

// SecurityHeaders applies the response headers to every route, including
// static assets and error pages. Handlers may override Cache-Control (the
// hashed asset bundle does); everything else is fixed.
func SecurityHeaders(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	hsts := ""
	if cfg.HSTSMaxAgeSeconds > 0 {
		hsts = fmt.Sprintf("max-age=%d; includeSubDomains", cfg.HSTSMaxAgeSeconds)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			// The fragment is never sent in a Referer by any browser, but
			// the paste ID in the path is — and that ID is what identifies
			// the secret. Send no referrer at all.
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Cross-Origin-Embedder-Policy", "require-corp")
			h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=(), interest-cohort=()")
			// Share links are meant to be handed to one person, not
			// crawled and archived.
			h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
			// Default for everything; the static asset handler relaxes this
			// for content-hashed bundles only.
			h.Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			h.Set("Pragma", "no-cache")
			if hsts != "" {
				h.Set("Strict-Transport-Security", hsts)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSameOrigin rejects state-changing requests that a browser tells us
// came from somewhere else. The API has no cookies or ambient credentials,
// so cross-site posting is not a classic CSRF risk — but this makes another
// site driving paste creation through a victim's browser (burning their
// quota, laundering the source address of abuse) a non-starter, and costs
// one header comparison.
//
// Non-browser clients (curl, scripts) send neither header and are allowed
// through: the check can only tighten what browsers do.
func RequireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
			// Absent (older browser, non-browser client), or genuinely
			// first-party.
		default:
			writeError(w, ErrCrossOrigin)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r) {
			writeError(w, ErrCrossOrigin)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originMatchesHost(origin string, r *http.Request) bool {
	// Compare host:port only; the scheme is not knowable from inside a
	// TLS-terminating proxy and adds nothing to this check.
	trimmed := origin
	if i := strings.Index(trimmed, "://"); i >= 0 {
		trimmed = trimmed[i+3:]
	}
	return strings.EqualFold(trimmed, r.Host)
}
