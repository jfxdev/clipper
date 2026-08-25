package api

import "net/http"

// NewRouter wires the REST API. Rate limiting is applied to every endpoint:
// creation is the main abuse vector (storage exhaustion), GET is limited to
// slow down brute-forcing of paste IDs and read tokens, and health is
// limited so it can't be used as an unmetered amplification target.
//
// mode restricts which operations this instance serves: "read" disables
// creation, "write" disables retrieval, and the empty default serves both.
// A disabled operation answers 403 with a generic body that never names the
// mode, so clients can localize the condition from the status code without
// the server leaking its deployment topology.
func NewRouter(h *Handlers, rl *RateLimiter, mode string) http.Handler {
	mux := http.NewServeMux()

	create := http.Handler(RequireSameOrigin(http.HandlerFunc(h.CreatePaste)))
	if mode == "read" {
		create = http.HandlerFunc(modeDisabled)
	}
	get := http.Handler(http.HandlerFunc(h.GetPaste))
	if mode == "write" {
		get = http.HandlerFunc(modeDisabled)
	}

	mux.Handle("POST /api/paste", rl.Middleware(create))
	mux.Handle("GET /api/paste/{id}", rl.Middleware(get))
	// Health stays available in every mode: the container probe in
	// runHealthcheck depends on it regardless of read/write gating.
	mux.Handle("GET /api/health", rl.Middleware(http.HandlerFunc(h.Health)))
	// The SPA reads this on load to render the right UI (e.g. hide the create
	// form on a read-only instance). It reports capabilities, not the mode
	// name. Available in every mode so both split instances can serve it.
	mux.Handle("GET /api/config", rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, clientConfigResponse{
			CreateEnabled: mode != "read",
			ReadEnabled:   mode != "write",
		})
	})))
	// Anything else under /api/ is a 404 in JSON rather than falling
	// through to the SPA's index.html, which would answer an API probe with
	// a 200 and an HTML body.
	mux.Handle("/api/", rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "not found"})
	})))
	return mux
}

// modeDisabled answers an operation this instance's MODE does not serve. The
// body is deliberately generic — it never names "read"/"write" or the mode —
// so the response reveals only that the operation is not available here, not
// how the deployment is split.
func modeDisabled(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusForbidden, errorResponse{Error: "forbidden"})
}
