package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

var (
	// ErrRateLimited is returned by RateLimiter when a client has exceeded
	// its request budget.
	ErrRateLimited = errors.New("api: rate limit exceeded")

	// ErrQuotaExceeded is returned when a client has used up its storage
	// allowance for the current window.
	ErrQuotaExceeded = errors.New("api: storage quota exceeded")

	// ErrCrossOrigin is returned when a browser tells us a state-changing
	// request came from another site.
	ErrCrossOrigin = errors.New("api: cross-origin request rejected")
)

// errorLogger is where writeError sends unexpected failures. It is a
// package-level variable so main can install the structured logger without
// threading it through every handler signature.
var errorLogger = slog.Default()

// SetErrorLogger installs the logger used for server-side error reporting.
func SetErrorLogger(l *slog.Logger) { errorLogger = l }

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// Responses carry ciphertext and, for a burned paste, content that no
	// longer exists anywhere else. Nothing about them should survive in a
	// shared proxy, a CDN, or the browser's disk cache.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError maps a domain error to an HTTP status and a safe JSON body.
// Client errors (bad input, not found, rate limited) get a message
// describing what's wrong; unexpected errors get a generic message and are
// logged server-side instead, so internals are never leaked to callers. The
// paste's plaintext/ciphertext content is never part of any error, so it
// never ends up in these responses or logs either.
func writeError(w http.ResponseWriter, err error) {
	status, msg := classify(err)
	if status == http.StatusInternalServerError {
		errorLogger.Error("api: internal error", slog.Any("error", err))
		msg = "internal server error"
	}
	writeJSON(w, status, errorResponse{Error: msg})
}

func classify(err error) (status int, msg string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, "paste not found"
	case errors.Is(err, paste.ErrEmpty):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, paste.ErrMalformed):
		// The specific reason is safe to return: it describes the caller's
		// own request, and the envelope structure is public by design.
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, paste.ErrInvalidID), errors.Is(err, paste.ErrInvalidToken):
		// Deliberately indistinguishable from a real miss, so a malformed
		// probe and a valid-but-absent ID look identical to a scanner.
		return http.StatusNotFound, "paste not found"
	case errors.Is(err, paste.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests, "too many requests"
	case errors.Is(err, ErrQuotaExceeded):
		return http.StatusTooManyRequests, "storage quota exceeded, try again later"
	case errors.Is(err, ErrCrossOrigin):
		return http.StatusForbidden, "cross-origin request rejected"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
