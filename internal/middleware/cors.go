// Package middleware holds the chi middleware chain components. Each
// middleware sits at the boundary of the BFF and is plain net/http so it can
// be tested in isolation and chained in any order.
package middleware

import (
	"net/http"
	"strings"

	"github.com/asolovov/evm-oracle-demo-api/config"
)

// CORS returns a middleware that enforces the configured CORS policy.
// `cfg.CORSAllowAll = true` echoes whatever Origin the client sent (useful
// in dev). Otherwise only origins listed in `cfg.CORSOrigins` are honored.
func CORS(cfg config.HTTPConfig) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(cfg.CORSOrigins))
	for _, o := range cfg.CORSOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" {
				allow := cfg.CORSAllowAll
				if !allow {
					_, allow = allowed[origin]
				}
				if allow {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
					w.Header().Set("Access-Control-Max-Age", "600")
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
