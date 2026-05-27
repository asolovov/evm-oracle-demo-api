package ratelimit

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// Middleware enforces the supplied Limiter per ClientIP. Bypassed paths skip
// the check entirely — useful for /api/v1/health which should always answer
// regardless of upstream traffic.
func Middleware(limiter Limiter, trustedProxies []string, bypass []string) func(http.Handler) http.Handler {
	bypassSet := make(map[string]struct{}, len(bypass))
	for _, p := range bypass {
		bypassSet[strings.TrimRight(p, "/")] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, skip := bypassSet[strings.TrimRight(r.URL.Path, "/")]; skip {
				next.ServeHTTP(w, r)
				return
			}

			ip := ClientIP(r, trustedProxies)
			decision, err := limiter.Allow(r.Context(), ip)
			if err != nil {
				// Fail open — a Redis outage shouldn't take the public
				// surface down. The error is logged at warn level.
				logger.Log().WithError(err).WithField("client_ip", ip).Warn("ratelimit: limiter errored, failing open")
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))

			if !decision.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(decision.RetryAfter.Seconds())))
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":        "rate_limited",
					"message":     "request rate exceeded; retry after Retry-After seconds",
					"retry_after": int(decision.RetryAfter.Seconds()),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
