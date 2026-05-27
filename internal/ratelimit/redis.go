package ratelimit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// slidingWindowScript implements a sliding-window-log limiter via a Redis
// sorted set keyed per client IP. Each request adds a token at the current
// millisecond; older tokens are evicted on every call so the active count
// is the request rate over the trailing `window_ms` milliseconds.
//
//   - KEYS[1]   bucket key, e.g. `ratelimit:1.2.3.4`.
//   - ARGV[1]   now in milliseconds since epoch.
//   - ARGV[2]   window length in milliseconds.
//   - ARGV[3]   max requests allowed within the window (rate + burst).
//   - ARGV[4]   unique token id (one per request) — gives ZADD a distinct member.
//
// Returns {allowed (1/0), current_count, retry_after_ms}. retry_after_ms is
// 0 on accept and the time until the oldest still-in-window token expires
// on reject. PEXPIRE keeps the key from sticking around when an IP goes
// quiet.
const slidingWindowScript = `
local key = KEYS[1]
local now_ms = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local token = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms - window_ms)
local count = redis.call('ZCARD', key)
if count >= limit then
    local first = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retry_ms = window_ms
    if first and #first >= 2 then
        retry_ms = tonumber(first[2]) + window_ms - now_ms
        if retry_ms < 0 then retry_ms = 0 end
    end
    return {0, count, retry_ms}
end

redis.call('ZADD', key, now_ms, token)
redis.call('PEXPIRE', key, window_ms)
return {1, count + 1, 0}
`

// RedisLimiter implements Limiter on top of go-redis. One Lua script handles
// the trim + insert + count + retry-after computation so the increment is
// race-free across the listener's worker pool and across multiple BFF
// replicas hitting the same Redis.
type RedisLimiter struct {
	client    redis.UniversalClient
	script    *redis.Script
	limit     int
	windowMS  int
	keyPrefix string
	nowFn     func() time.Time
}

// NewRedisLimiter constructs a RedisLimiter. Pass requestsPerMinute + burst
// in their raw form; the limiter sums them into the per-window cap so an
// occasional spike up to (rate + burst) lands without 429s.
func NewRedisLimiter(client redis.UniversalClient, requestsPerMinute, burst int) *RedisLimiter {
	return &RedisLimiter{
		client:    client,
		script:    redis.NewScript(slidingWindowScript),
		limit:     requestsPerMinute + burst,
		windowMS:  60 * 1000,
		keyPrefix: "ratelimit",
		nowFn:     time.Now,
	}
}

// Allow consults Redis for the given client key. Returns Allowed=true if the
// trailing-window count stays within (rate + burst), false otherwise with
// Retry-After populated from the time until the oldest in-window token
// expires.
func (l *RedisLimiter) Allow(ctx context.Context, key string) (Decision, error) {
	nowMS := l.nowFn().UnixMilli()
	token := strconv.FormatInt(nowMS, 10) + ":" + uuid.NewString()
	redisKey := l.keyPrefix + ":" + key

	raw, err := l.script.Run(ctx, l.client, []string{redisKey},
		nowMS, l.windowMS, l.limit, token,
	).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit script: %w", err)
	}
	values, ok := raw.([]any)
	if !ok || len(values) < 3 {
		return Decision{}, fmt.Errorf("ratelimit script: unexpected reply shape %T", raw)
	}
	allowedRaw, _ := values[0].(int64)
	current, _ := values[1].(int64)
	retryMS, _ := values[2].(int64)

	allowed := allowedRaw == 1
	remaining := int(int64(l.limit) - current)
	if remaining < 0 {
		remaining = 0
	}
	d := Decision{
		Allowed:   allowed,
		Limit:     l.limit,
		Remaining: remaining,
	}
	if !allowed {
		d.RetryAfter = time.Duration(retryMS) * time.Millisecond
		// Always surface at least a 1s Retry-After so the client doesn't
		// hot-loop when retryMS rounds down to zero on a boundary tick.
		if d.RetryAfter < time.Second {
			d.RetryAfter = time.Second
		}
	}
	return d, nil
}
