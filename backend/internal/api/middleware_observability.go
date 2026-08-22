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
				slog.String("client", sanitizeForLog(clientIP(r, trustProxy, trusted))),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// maxLoggedPathLen bounds how much of an arbitrary request path reaches the
// log. Paths are attacker-chosen and unbounded; without a cap, a few
// requests can write megabytes into a log pipeline.
const maxLoggedPathLen = 128

// redactPath strips the paste ID out of a request path before it is
// logged, leaving the route shape intact, and makes whatever is left safe
// to write into a log line.
func redactPath(path string) string {
	const prefix = "/api/paste/"
	if strings.HasPrefix(path, prefix) && len(path) > len(prefix) {
		return prefix + "{id}"
	}
	if strings.HasPrefix(path, "/paste/") && len(path) > len("/paste/") {
		return "/paste/{id}"
	}
	return sanitizeForLog(path)
}

// sanitizeForLog makes an untrusted string safe to record.
//
// Two problems, both from the same source. A path (or any request-derived
// value) can carry newlines and terminal control sequences, which in a
// line-oriented log let an attacker forge entries that look like they came
// from the server — CWE-117. And it can be arbitrarily long, which turns a
// handful of requests into a flooded log. The JSON handler already escapes
// control characters, so forged *lines* are not reachable today; this makes
// the property hold regardless of which handler is installed, and bounds
// the size either way.
func sanitizeForLog(value string) string {
	if len(value) > maxLoggedPathLen {
		value = value[:maxLoggedPathLen] + "…"
	}
	// Line terminators first and explicitly: they are the characters that
	// actually forge a log entry, and spelling them out keeps the intent
	// obvious to a reader (and to static analysis) rather than hiding it
	// inside the rune predicate below.
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return strings.Map(func(r rune) rune {
		// Then the rest of the characters a terminal or log viewer acts on:
		// remaining C0 controls, DEL, and the C1 range. Unicode graphics
		// are kept — mangling them would make legitimate paths unreadable
		// without adding any safety.
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, value)
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
