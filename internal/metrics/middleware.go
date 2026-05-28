package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// metricsRecorder wraps a ResponseWriter to capture the status code for the
// per-route counter + histogram labels. Delegates Hijack + Flush to the
// underlying writer so WebSocket upgrades and streaming responses still work
// when this middleware sits in the chain.
type metricsRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *metricsRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

// Hijack passes the call through to the underlying writer when it supports
// it, so WebSocket / SSE handlers can take over the connection.
func (s *metricsRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("metrics.metricsRecorder: underlying ResponseWriter does not implement http.Hijacker")
	}
	if !s.wrote {
		s.status = http.StatusSwitchingProtocols
		s.wrote = true
	}
	return h.Hijack()
}

// Flush forwards to the underlying writer's Flusher when present.
func (s *metricsRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Middleware records http_requests_total + http_request_duration_seconds.
// The route template (e.g. "/api/v1/assets/{id}/price") is used as the
// `path` label so cardinality stays bounded — never the raw URL.
func (m *Metrics) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &metricsRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start).Seconds()

			path := chi.RouteContext(r.Context()).RoutePattern()
			if path == "" {
				path = "unknown"
			}

			m.HTTPRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rec.status)).Inc()
			m.HTTPRequestDurationS.WithLabelValues(r.Method, path).Observe(elapsed)
		})
	}
}
