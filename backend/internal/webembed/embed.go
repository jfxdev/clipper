// Package webembed embeds the built React SPA (frontend/dist, copied here
// by `make build-frontend`) into the Go binary so a single process/container
// can serve both the REST API and the static frontend.
package webembed

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded SPA. Requests for a real static file (JS, CSS,
// images) are served directly; everything else falls back to index.html so
// client-side routes like /paste/:id survive a hard refresh or a shared
// link opened directly.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, err
	}

	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		// The frontend hasn't been built into this tree yet (e.g. running
		// `go test`/`go run` without `make build-frontend` first). Fail
		// soft so backend-only development keeps working.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not built: run `make build-frontend` first", http.StatusNotFound)
		}), nil
	}

	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if info, err := fs.Stat(sub, path); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}), nil
}
