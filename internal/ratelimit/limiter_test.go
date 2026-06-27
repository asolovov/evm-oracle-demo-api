package ratelimit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniRedis(t *testing.T) (*RedisLimiter, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewRedisLimiter(client, 5, 2), mr
}

func newControlledClock(t *testing.T) (*RedisLimiter, *clock) {
	t.Helper()
	limiter, _ := newMiniRedis(t)
	c := &clock{now: time.Unix(1_700_000_000, 0)}
	limiter.nowFn = c.Now
	return limiter, c
}

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestAllowFillsBucketThenRejects(t *testing.T) {
	limiter, _ := newMiniRedis(t)
	ctx := context.Background()

	// Limit + burst = 5 + 2 = 7 per window. First 7 must pass, 8th rejects.
	for i := 1; i <= 7; i++ {
		d, err := limiter.Allow(ctx, "1.2.3.4")
		if err != nil {
			t.Fatalf("Allow #%d: %v", i, err)
		}
		if !d.Allowed {
			t.Fatalf("expected request #%d to be allowed", i)
		}
	}

	d, err := limiter.Allow(ctx, "1.2.3.4")
	if err != nil {
		t.Fatalf("Allow #8: %v", err)
	}
	if d.Allowed {
		t.Fatalf("expected 8th request to be rejected, got Allowed=true")
	}
	if d.RetryAfter <= 0 {
		t.Fatalf("expected RetryAfter > 0 on reject, got %v", d.RetryAfter)
	}
}

func TestAllowWindowSlidesForward(t *testing.T) {
	// Verifies the sliding-window semantics: tokens older than the window
	// are evicted as wall-clock time advances, so the bucket recovers
	// without waiting for the fixed-window boundary.
	limiter, clk := newControlledClock(t)
	ctx := context.Background()

	// Burn the full limit at t=0. 5 + 2 = 7.
	for i := 1; i <= 7; i++ {
		if d, _ := limiter.Allow(ctx, "1.2.3.4"); !d.Allowed {
			t.Fatalf("Allow #%d unexpectedly rejected", i)
		}
	}
	// At t=30s, still inside the 60s window — must reject.
	clk.Advance(30 * time.Second)
	if d, _ := limiter.Allow(ctx, "1.2.3.4"); d.Allowed {
		t.Fatalf("expected reject at t=30s (tokens still in window)")
	}
	// At t=61s, every token from t=0 has expired — must accept again.
	clk.Advance(31 * time.Second)
	if d, _ := limiter.Allow(ctx, "1.2.3.4"); !d.Allowed {
		t.Fatalf("expected accept at t=61s (tokens evicted)")
	}
}

func TestAllowRetryAfterShrinksAsWindowDrains(t *testing.T) {
	limiter, clk := newControlledClock(t)
	ctx := context.Background()

	// Burn the limit.
	for i := 1; i <= 7; i++ {
		if _, err := limiter.Allow(ctx, "1.2.3.4"); err != nil {
			t.Fatalf("Allow: %v", err)
		}
	}
	// First reject at t=0 → Retry-After should be ~60s (rounded to the
	// floor with the 1s minimum applied).
	d, _ := limiter.Allow(ctx, "1.2.3.4")
	if d.Allowed || d.RetryAfter < 59*time.Second {
		t.Fatalf("expected reject with Retry-After >= 59s, got Allowed=%v Retry=%v", d.Allowed, d.RetryAfter)
	}
	// Advance 30s — Retry-After should now be ~30s.
	clk.Advance(30 * time.Second)
	d, _ = limiter.Allow(ctx, "1.2.3.4")
	if d.Allowed || d.RetryAfter > 31*time.Second {
		t.Fatalf("expected reject with Retry-After <= 31s at t=30s, got Allowed=%v Retry=%v", d.Allowed, d.RetryAfter)
	}
}

func TestAllowKeysSeparateIPs(t *testing.T) {
	limiter, _ := newMiniRedis(t)
	ctx := context.Background()

	for i := 0; i < 7; i++ {
		if d, _ := limiter.Allow(ctx, "1.2.3.4"); !d.Allowed {
			t.Fatalf("a #%d unexpectedly rejected", i)
		}
	}
	// Different IP should still have its full bucket.
	if d, _ := limiter.Allow(ctx, "5.6.7.8"); !d.Allowed {
		t.Fatalf("b expected to be allowed (separate bucket), got rejected")
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	limiter, _ := newMiniRedis(t)

	mw := Middleware(limiter, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// 8th request triggers 429.
	for i := 1; i <= 7; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request #%d expected 200, got %d", i, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ra := rec.Header().Get("Retry-After"); ra == "" {
		t.Fatalf("expected Retry-After header on 429")
	} else if v, err := strconv.Atoi(ra); err != nil || v <= 0 {
		t.Fatalf("Retry-After %q is not a positive integer", ra)
	}
	if lim := rec.Header().Get("X-RateLimit-Limit"); lim != "7" {
		t.Fatalf("X-RateLimit-Limit = %q, want 7", lim)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode 429 body: %v", err)
	}
	if body["code"] != "rate_limited" {
		t.Fatalf("body code = %v, want rate_limited", body["code"])
	}
}

func TestMiddlewareBypassesHealth(t *testing.T) {
	limiter, _ := newMiniRedis(t)
	mw := Middleware(limiter, nil, []string{"/api/v1/health"})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Many requests to the bypass path should never 429.
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("health request #%d expected 200, got %d", i, rec.Code)
		}
	}
}

func TestClientIPRespectsTrustedProxy(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		xff        string
		trusted    []string
		want       string
	}{
		{"untrusted_no_xff", "203.0.113.4:9999", "", nil, "203.0.113.4"},
		{"untrusted_with_xff", "203.0.113.4:9999", "1.2.3.4", nil, "203.0.113.4"},
		{"trusted_with_xff", "10.0.0.1:9999", "1.2.3.4", []string{"10.0.0.1"}, "1.2.3.4"},
		{"trusted_xff_chain", "10.0.0.1:9999", "1.2.3.4, 10.0.0.5", []string{"10.0.0.1"}, "1.2.3.4"},
		{"trusted_cidr", "10.0.0.2:9999", "1.2.3.4", []string{"10.0.0.0/24"}, "1.2.3.4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			got := ClientIP(req, tc.trusted)
			if got != tc.want {
				t.Fatalf("ClientIP = %q, want %q (xff=%q trusted=%v)", got, tc.want, tc.xff, tc.trusted)
			}
		})
	}
}

func TestMiddlewareFailsOpenOnLimiterError(t *testing.T) {
	mw := Middleware(brokenLimiter{}, nil, nil)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected fail-open 200, got %d", rec.Code)
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(_ context.Context, _ string) (Decision, error) {
	return Decision{}, errFake
}

var errFake = stringError("boom")

type stringError string

func (s stringError) Error() string { return string(s) }

// silence unused-import diagnostic when miniredis package is only used by
// the helper above.
var _ = strings.TrimSpace
