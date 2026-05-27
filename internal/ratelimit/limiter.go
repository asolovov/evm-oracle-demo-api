// Package ratelimit wraps a Redis-backed fixed-window + burst limiter
// behind a chi middleware. Per-IP keying respects the configured
// trusted-proxy list when extracting the client IP.
package ratelimit

import (
	"context"
	"time"
)

// Decision describes a single allow/deny verdict.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
}

// Limiter is the surface the middleware consumes. Mockable for unit tests.
type Limiter interface {
	Allow(ctx context.Context, key string) (Decision, error)
}
