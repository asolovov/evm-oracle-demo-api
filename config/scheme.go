// Package config defines application configuration defaults and schema.
package config

import (
	"errors"
	"fmt"
)

// Scheme is the BFF's configuration scheme. Every nested key is registered in
// init.go via viper.SetDefault (architecture rule 6: viper.AutomaticEnv alone
// does not populate nested keys on Unmarshal — every key must be in viper's
// namespace before load).
type Scheme struct {
	HTTP       HTTPConfig       `mapstructure:"http"`
	Healthz    HealthzConfig    `mapstructure:"healthz"`
	Redis      RedisConfig      `mapstructure:"redis"`
	GRPCClient GRPCClientConfig `mapstructure:"grpc_client"`
	RateLimit  RateLimitConfig  `mapstructure:"rate_limit"`
	Author     AuthorConfig     `mapstructure:"author"`
	Telemetry  TelemetryConfig  `mapstructure:"telemetry"`

	// Env is the application environment (e.g. prod, dev, local). Drives
	// permissive CORS defaults and similar dev-only fallbacks.
	Env string `mapstructure:"env"`
}

// HTTPConfig holds the public HTTP listener settings.
type HTTPConfig struct {
	Host           string   `mapstructure:"host"`
	Port           int      `mapstructure:"port"`
	ReadTimeout    string   `mapstructure:"read_timeout"`
	WriteTimeout   string   `mapstructure:"write_timeout"`
	IdleTimeout    string   `mapstructure:"idle_timeout"`
	CORSOrigins    []string `mapstructure:"cors_origins"`
	CORSAllowAll   bool     `mapstructure:"cors_allow_all"`
	TrustedProxies []string `mapstructure:"trusted_proxies"`
}

// HealthzConfig holds the operator-facing listener settings. Health + metrics
// run on a dedicated port so they're easy to firewall off from the public LB.
type HealthzConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// RedisConfig holds the Redis connection settings for rate-limit + cache state.
// Redis is the only persistent dependency this service has — there is no
// relational DB (architecture rule 7 deviation; documented in the README).
type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// GRPCClientConfig holds dial targets for the two upstream services this BFF
// fans out to. External client wrappers are plain packages, not template
// modules (architecture rule 5).
type GRPCClientConfig struct {
	PriceServiceAddr   string          `mapstructure:"price_service_addr"`
	IndexerServiceAddr string          `mapstructure:"indexer_service_addr"`
	OracleServiceAddr  string          `mapstructure:"oracle_service_addr"`
	DialTimeout        string          `mapstructure:"dial_timeout"`
	KeepAlive          KeepAliveConfig `mapstructure:"keep_alive"`
	UseTLS             bool            `mapstructure:"use_tls"`
	Subscribe          SubscribeConfig `mapstructure:"subscribe"`
}

// KeepAliveConfig holds gRPC keep-alive settings shared by every client.
type KeepAliveConfig struct {
	Time                string `mapstructure:"time"`
	Timeout             string `mapstructure:"timeout"`
	PermitWithoutStream bool   `mapstructure:"permit_without_stream"`
}

// SubscribeConfig controls the long-lived streaming subscriptions backing the
// /ws/stream endpoint.
type SubscribeConfig struct {
	// AssetIDs the WS hub subscribes to on price.Subscribe. Empty means
	// "all configured assets" — the registry of asset IDs lives in
	// internal/models.
	AssetIDs []string `mapstructure:"asset_ids"`
	// ReconnectBackoff controls the upstream-stream re-establish interval
	// after a transient error.
	ReconnectBackoff string `mapstructure:"reconnect_backoff"`
}

// RateLimitConfig holds the IP-based rate-limit settings backed by Redis.
type RateLimitConfig struct {
	Enabled           bool `mapstructure:"enabled"`
	RequestsPerMinute int  `mapstructure:"requests_per_minute"`
	BurstSize         int  `mapstructure:"burst_size"`
}

// AuthorConfig surfaces the credential block returned by GET /api/v1/health.
// Drives the portfolio-surface requirement (FR-09).
type AuthorConfig struct {
	Name  string            `mapstructure:"name"`
	Links map[string]string `mapstructure:"links"`
}

// TelemetryConfig holds the logger + future tracing settings.
type TelemetryConfig struct {
	LogLevel  string `mapstructure:"log_level"`
	LogFormat string `mapstructure:"log_format"`
}

// Validate fails fast on configuration errors so an orchestrator's crash-loop
// surfaces a misconfigured key instead of a half-up service.
func (s *Scheme) Validate() error {
	var errs []error

	if s.HTTP.Port <= 0 || s.HTTP.Port > 65535 {
		errs = append(errs, fmt.Errorf("http.port must be between 1 and 65535, got %d", s.HTTP.Port))
	}
	if s.Healthz.Port <= 0 || s.Healthz.Port > 65535 {
		errs = append(errs, fmt.Errorf("healthz.port must be between 1 and 65535, got %d", s.Healthz.Port))
	}
	if s.HTTP.Port == s.Healthz.Port {
		errs = append(errs, fmt.Errorf("http.port and healthz.port must differ (both = %d)", s.HTTP.Port))
	}
	if s.Redis.Addr == "" {
		errs = append(errs, errors.New("redis.addr is required"))
	}
	if s.GRPCClient.PriceServiceAddr == "" {
		errs = append(errs, errors.New("grpc_client.price_service_addr is required"))
	}
	if s.GRPCClient.IndexerServiceAddr == "" {
		errs = append(errs, errors.New("grpc_client.indexer_service_addr is required"))
	}
	if s.GRPCClient.OracleServiceAddr == "" {
		errs = append(errs, errors.New("grpc_client.oracle_service_addr is required"))
	}
	if s.RateLimit.Enabled && s.RateLimit.RequestsPerMinute <= 0 {
		errs = append(errs, fmt.Errorf("rate_limit.requests_per_minute must be > 0 when enabled, got %d", s.RateLimit.RequestsPerMinute))
	}
	if s.Author.Name == "" {
		// Warn-only by surfacing as a validation error — this keeps the
		// portfolio surface honest (FR-09). Operators can suppress by
		// setting AUTHOR_NAME explicitly to whatever they want exposed.
		errs = append(errs, errors.New("author.name is required (drives the /api/v1/health credential block)"))
	}

	return errors.Join(errs...)
}
