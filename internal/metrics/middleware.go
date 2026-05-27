package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

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
