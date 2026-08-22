package api

import "net/http"

// NewRouter wires the REST API. Rate limiting is applied to every endpoint:
// creation is the main abuse vector (storage exhaustion), GET is limited to
// slow down brute-forcing of paste IDs and read tokens, and health is
// limited so it can't be used as an unmetered amplification target.
func NewRouter(h *Handlers, rl *RateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/paste", rl.Middleware(RequireSameOrigin(http.HandlerFunc(h.CreatePaste))))
	mux.Handle("GET /api/paste/{id}", rl.Middleware(http.HandlerFunc(h.GetPaste)))
	mux.Handle("GET /api/health", rl.Middleware(http.HandlerFunc(h.Health)))
	// Anything else under /api/ is a 404 in JSON rather than falling
	// through to the SPA's index.html, which would answer an API probe with
	// a 200 and an HTML body.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
	})
	return mux
}
