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

// hashedAssetPrefix is the directory Vite writes content-hashed bundles to.
// Those filenames change whenever their contents change, so they are the
// only responses safe to cache — everything else (index.html above all)
// keeps the no-store default set by the security middleware.
const hashedAssetPrefix = "assets/"

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
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" && fs.ValidPath(path) {
			if info, err := fs.Stat(sub, path); err == nil && !info.IsDir() {
				if strings.HasPrefix(path, hashedAssetPrefix) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}), nil
}

// securityTxt is served at /.well-known/security.txt (RFC 9116) so someone
// who finds a flaw has an unambiguous, machine-readable way to report it
// instead of guessing at an address or posting it publicly.
const securityTxt = `Contact: https://github.com/jfxdev/clipper/security/advisories/new
Expires: 2030-01-01T00:00:00.000Z
Preferred-Languages: pt-BR, en
Canonical: /.well-known/security.txt
Policy: https://github.com/jfxdev/clipper/blob/main/SECURITY.md
`

// SecurityTxtHandler serves the RFC 9116 disclosure record.
func SecurityTxtHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(securityTxt))
	})
}
