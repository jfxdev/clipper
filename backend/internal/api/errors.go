package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

// ErrRateLimited is returned by RateLimiter when a client has exceeded its
// request budget.
var ErrRateLimited = errors.New("api: rate limit exceeded")

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
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
		log.Printf("api: internal error: %v", err)
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
	case errors.Is(err, paste.ErrTooLarge):
		return http.StatusRequestEntityTooLarge, err.Error()
	case errors.Is(err, ErrRateLimited):
		return http.StatusTooManyRequests, "too many requests"
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
