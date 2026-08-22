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

// readTokenHeader carries the client's read token. A custom header is used
// rather than a query parameter for two reasons: query strings end up in
// proxy and access logs in a way headers do not, and requiring a custom
// header makes the request non-simple, so a cross-origin read is stopped by
// the browser's preflight before it ever reaches us.
// #nosec G101 -- this is the name of an HTTP header, not a credential.
const readTokenHeader = "X-Paste-Read-Token"

// HandlersConfig configures NewHandlers.
type HandlersConfig struct {
	MaxPasteSizeBytes int64

	// MaxExpireSeconds is the longest lifetime a paste may request. Every
	// paste must expire: an unbounded secret is a liability that outlives
	// the reason it was shared.
	MaxExpireSeconds int64

	// Quota, when set, charges each accepted paste against the client's
	// storage allowance.
	Quota *Quota
}

type Handlers struct {
	store store.Store
	cfg   HandlersConfig
}

func NewHandlers(s store.Store, cfg HandlersConfig) *Handlers {
	return &Handlers{store: s, cfg: cfg}
}

func (h *Handlers) CreatePaste(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxPasteSizeBytes+requestOverhead)

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

	if req.ExpireSeconds <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "expireSeconds must be positive: pastes must expire"})
		return
	}
	if req.ExpireSeconds > h.cfg.MaxExpireSeconds {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "expireSeconds exceeds the maximum retention window"})
		return
	}
	if err := paste.ValidateReadToken(req.ReadToken); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "readToken must be a base64url SHA-256 digest"})
		return
	}
	if err := paste.Validate(req.Data, h.cfg.MaxPasteSizeBytes); err != nil {
		writeError(w, err)
		return
	}

	// Charged only after validation, so malformed requests can't burn a
	// client's allowance, and before the write, so the allowance actually
	// bounds what reaches storage.
	if h.cfg.Quota != nil && !h.cfg.Quota.Charge(r, int64(len(req.Data))) {
		writeError(w, ErrQuotaExceeded)
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
		ReadToken:         req.ReadToken,
		BurnAfterRead:     req.BurnAfterRead,
		PasswordProtected: req.PasswordProtected,
		CreatedAt:         now,
		SizeBytes:         len(req.Data),
		ExpiresAt:         now.Add(time.Duration(req.ExpireSeconds) * time.Second),
	}

	if err := h.store.Create(r.Context(), p); err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, createPasteResponse{ID: id})
}

func (h *Handlers) GetPaste(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := paste.ValidateID(id); err != nil {
		writeError(w, err)
		return
	}
	token := r.Header.Get(readTokenHeader)
	if err := paste.ValidateReadToken(token); err != nil {
		writeError(w, err)
		return
	}

	p, err := h.store.Get(r.Context(), id, token)
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
