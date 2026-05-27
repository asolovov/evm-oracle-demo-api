package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// Recovery converts a panic into a 500 response and logs the stack trace.
// Without this middleware a panic in a handler crashes the server process.
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Log().WithFields(map[string]interface{}{
						"path":  r.URL.Path,
						"panic": rec,
						"stack": string(debug.Stack()),
					}).Error("handler panic — recovered into HTTP 500")
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
