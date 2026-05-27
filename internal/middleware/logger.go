package middleware

import (
	"net/http"
	"time"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// statusRecorder wraps a ResponseWriter to capture the eventual status code
// for the access log line.
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
