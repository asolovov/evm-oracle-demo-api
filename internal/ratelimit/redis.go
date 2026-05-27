package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// fixedWindowScript implements a per-IP fixed-window limiter with burst.
//
//   - KEYS[1] is the bucket key, e.g. `ratelimit:1.2.3.4:bucket-481234`.
//   - ARGV[1] is the maximum count allowed within the window (requests/min +
//     burst).
//   - ARGV[2] is the window length in seconds (60 for per-minute).
//
// Returns {current_count, ttl_seconds}.
const fixedWindowScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, window)
end
local ttl = redis.call('TTL', key)
if ttl < 0 then
    redis.call('EXPIRE', key, window)
    ttl = window
end
return {current, ttl, limit}
`

// RedisLimiter implements Limiter on top of go-redis. One Lua script handles
// the increment + TTL set so the limiter remains race-free across the
// listener's worker pool.
type RedisLimiter struct {
	client      redis.UniversalClient
	script      *redis.Script
	limit       int
	windowSec   int
	keyPrefix   string
}

// NewRedisLimiter constructs a RedisLimiter. Pass requestsPerMinute + burst
// in their raw form; the limiter sums them into the per-window cap so an
// occasional spike up to (rate + burst) lands without 429s.
func NewRedisLimiter(client redis.UniversalClient, requestsPerMinute, burst int) *RedisLimiter {
	return &RedisLimiter{
		client:    client,
		script:    redis.NewScript(fixedWindowScript),
		limit:     requestsPerMinute + burst,
		windowSec: 60,
		keyPrefix: "ratelimit",
	}
}

// Allow consults Redis for the given client key. Returns Allowed=true if the
// post-increment count stays within (rate + burst), false otherwise with the
// Retry-After populated from the bucket TTL.
func (l *RedisLimiter) Allow(ctx context.Context, key string) (Decision, error) {
	bucket := time.Now().Unix() / int64(l.windowSec)
	redisKey := fmt.Sprintf("%s:%s:%d", l.keyPrefix, key, bucket)

	raw, err := l.script.Run(ctx, l.client, []string{redisKey}, l.limit, l.windowSec).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit script: %w", err)
	}
	values, ok := raw.([]any)
	if !ok || len(values) < 3 {
		return Decision{}, fmt.Errorf("ratelimit script: unexpected reply shape %T", raw)
	}
	current, _ := values[0].(int64)
	ttl, _ := values[1].(int64)
	limit, _ := values[2].(int64)

	allowed := current <= limit
	remaining := int(limit - current)
	if remaining < 0 {
		remaining = 0
	}
	d := Decision{
		Allowed:   allowed,
		Limit:     int(limit),
		Remaining: remaining,
	}
	if !allowed {
		d.RetryAfter = time.Duration(ttl) * time.Second
		if d.RetryAfter <= 0 {
			d.RetryAfter = time.Second
		}
	}
	return d, nil
}
