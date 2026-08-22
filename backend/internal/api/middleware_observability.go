package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

type ctxKey int

const requestIDKey ctxKey = 0

// RequestID attaches a short random id to each request and echoes it back.
// It is what makes an abuse report ("this response was wrong") traceable to
// a log line without logging anything about the paste itself.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b [9]byte
		id := "unknown"
		if _, err := rand.Read(b[:]); err == nil {
			id = base64.RawURLEncoding.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func requestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// statusRecorder captures the status code so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// AccessLog records one structured line per request. Without it there is no
// way to notice someone walking the ID space or hammering create: 404 and
// 429 rates are the signal, and they only exist if they are written down.
//
// The paste ID is deliberately redacted (see redactPath). It identifies a
// specific secret, and a log file or shipping pipeline is exactly the kind
// of place it should not accumulate.
func AccessLog(logger *slog.Logger, trustProxy bool, trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http request",
				slog.String("request_id", requestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", redactPath(r.URL.Path)),
				slog.Int("status", rec.status),
				slog.String("client", clientIP(r, trustProxy, trusted)),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// redactPath strips the paste ID out of a request path before it is
// logged, leaving the route shape intact.
func redactPath(path string) string {
	const prefix = "/api/paste/"
	if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
		return prefix + "{id}"
	}
	if strings.HasPrefix(path, "/paste/") && len(path) > len("/paste/") {
		return "/paste/{id}"
	}
	return path
}

// Recover turns a panic in any handler into a 500 instead of a killed
// connection, and keeps the stack out of the response body. Without it a
// single nil dereference in a handler takes down the goroutine mid-response
// and hands the client whatever bytes were already written.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.String("request_id", requestIDFrom(r.Context())),
						slog.String("path", redactPath(r.URL.Path)),
						slog.Any("panic", rec),
					)
					// Best effort: if the handler already wrote a status
					// this is a no-op with a warning, which is still better
					// than a truncated response with no explanation.
					writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
