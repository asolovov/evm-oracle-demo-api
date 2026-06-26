package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// statusRecorder wraps a ResponseWriter to capture the eventual status code
// for the access log line. Delegates Hijack + Flush to the underlying writer
// so the wrapper is transparent to WebSocket upgrades and streaming
// handlers — without these, gorilla/websocket's `w.(http.Hijacker)`
// assertion fails and the upgrade returns 500.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Hijack passes the call through to the underlying writer when it supports
// it, so WebSocket / SSE handlers can take over the connection.
func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("middleware.statusRecorder: underlying ResponseWriter does not implement http.Hijacker")
	}
	// A hijacked connection bypasses WriteHeader entirely; record the
	// canonical 101-style status so the access log line still reports
	// something coherent for streaming handlers.
	if !s.wrote {
		s.status = http.StatusSwitchingProtocols
		s.wrote = true
	}
	return h.Hijack()
}

// Flush forwards to the underlying writer's Flusher when present.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// AccessLog emits a single structured log line per request after the handler
// returns. Status defaults to 200 when the handler omits an explicit
// WriteHeader (mirroring net/http's own behavior).
func AccessLog() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Log().WithFields(map[string]interface{}{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status":      rec.status,
				"duration_ms": time.Since(start).Milliseconds(),
				"request_id":  RequestIDFromContext(r.Context()),
				"remote":      r.RemoteAddr,
			}).Info("http access")
		})
	}
}
