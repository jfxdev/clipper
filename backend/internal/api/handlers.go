package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

// requestOverhead accounts for the JSON envelope (field names, quoting,
// booleans, numbers) around the opaque Data payload, so a paste right at
// the configured size limit isn't rejected purely due to JSON framing.
const requestOverhead = 1024

// maxExpireSeconds caps how far in the future a paste can expire: one year,
// which is comfortably under the ~9.223e9-second threshold at which
// time.Duration(seconds) * time.Second would overflow int64 nanoseconds.
const maxExpireSeconds int64 = 365 * 24 * 60 * 60

type Handlers struct {
	store             store.Store
	maxPasteSizeBytes int64
}

func NewHandlers(s store.Store, maxPasteSizeBytes int64) *Handlers {
	return &Handlers{store: s, maxPasteSizeBytes: maxPasteSizeBytes}
}

func (h *Handlers) CreatePaste(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxPasteSizeBytes+requestOverhead)

	var req createPasteRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		// http.MaxBytesReader surfaces the size limit as a body-read error
		// during JSON decoding, not as a distinct step we can check first —
		// detect it here so oversized requests map to 413, not a generic 400.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, paste.ErrTooLarge)
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}
	// A second Decode must hit EOF — otherwise there's a second JSON value
	// or trailing garbage after the first one, which Decode alone ignores.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}
	if req.ExpireSeconds < 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "expireSeconds must not be negative"})
		return
	}
	if req.ExpireSeconds > maxExpireSeconds {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "expireSeconds is too large"})
		return
	}
	if err := paste.Validate(req.Data, h.maxPasteSizeBytes); err != nil {
		writeError(w, err)
		return
	}

	id, err := paste.NewID()
	if err != nil {
		writeError(w, err)
		return
	}

	now := time.Now().UTC()
	p := paste.Paste{
		ID:                id,
		Data:              req.Data,
		BurnAfterRead:     req.BurnAfterRead,
		PasswordProtected: req.PasswordProtected,
		CreatedAt:         now,
		SizeBytes:         len(req.Data),
	}
	if req.ExpireSeconds > 0 {
		p.ExpiresAt = now.Add(time.Duration(req.ExpireSeconds) * time.Second)
	}

	if err := h.store.Create(r.Context(), p); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createPasteResponse{ID: id})
}

func (h *Handlers) GetPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "missing paste id"})
		return
	}

	p, err := h.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, getPasteResponse{
		Data:              p.Data,
		BurnAfterRead:     p.BurnAfterRead,
		PasswordProtected: p.PasswordProtected,
		CreatedAt:         p.CreatedAt,
	})
}

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
