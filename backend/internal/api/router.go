package api

import "net/http"

// NewRouter wires the REST API. Rate limiting is applied to both endpoints:
// creation is the main abuse vector (storage exhaustion), and GET is also
// limited to slow down brute-forcing of paste IDs.
func NewRouter(h *Handlers, rl *RateLimiter) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /api/paste", rl.Middleware(http.HandlerFunc(h.CreatePaste)))
	mux.Handle("GET /api/paste/{id}", rl.Middleware(http.HandlerFunc(h.GetPaste)))
	mux.HandleFunc("GET /api/health", h.Health)
	return mux
}
