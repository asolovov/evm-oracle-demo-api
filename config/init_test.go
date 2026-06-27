package config

import (
	"testing"

	"github.com/spf13/viper"
)

func resetViper(t *testing.T) {
	t.Helper()
	viper.Reset()
	setDefaults()
}

func TestDefaultsCoverEverySchemeKey(t *testing.T) {
	resetViper(t)
	t.Cleanup(func() { viper.Reset() })

	// Each key under test corresponds to one mapstructure tag in scheme.go.
	// If a new field is added without a matching SetDefault, this test fails
	// loud — that's the whole point of architecture rule 6.
	for _, key := range []string{
		"env",
		"http.host", "http.port", "http.read_timeout", "http.write_timeout",
		"http.idle_timeout", "http.cors_origins", "http.cors_allow_all",
		"http.trusted_proxies",
		"healthz.host", "healthz.port",
		"redis.addr", "redis.password", "redis.db",
		"grpc_client.price_service_addr", "grpc_client.indexer_service_addr",
		"grpc_client.oracle_service_addr",
		"grpc_client.dial_timeout", "grpc_client.use_tls",
		"grpc_client.keep_alive.time", "grpc_client.keep_alive.timeout",
		"grpc_client.keep_alive.permit_without_stream",
		"grpc_client.subscribe.asset_ids", "grpc_client.subscribe.reconnect_backoff",
		"rate_limit.enabled", "rate_limit.requests_per_minute", "rate_limit.burst_size",
		"author.name", "author.links",
		"telemetry.log_level", "telemetry.log_format",
	} {
		if !viper.IsSet(key) {
			t.Errorf("expected SetDefault for key %q so Unmarshal can resolve it", key)
		}
	}
}

func TestValidateRejectsMissingRequired(t *testing.T) {
	t.Cleanup(func() { viper.Reset() })

	tests := []struct {
		name    string
		mutate  func(*Scheme)
		wantSub string
	}{
		{
			"redis_addr_empty",
			func(s *Scheme) { s.Redis.Addr = "" },
			"redis.addr",
		},
		{
			"price_addr_empty",
			func(s *Scheme) { s.GRPCClient.PriceServiceAddr = "" },
			"price_service_addr",
		},
		{
			"indexer_addr_empty",
			func(s *Scheme) { s.GRPCClient.IndexerServiceAddr = "" },
			"indexer_service_addr",
		},
		{
			"oracle_addr_empty",
			func(s *Scheme) { s.GRPCClient.OracleServiceAddr = "" },
			"oracle_service_addr",
		},
		{
			"author_name_empty",
			func(s *Scheme) { s.Author.Name = "" },
			"author.name",
		},
		{
			"port_collision",
			func(s *Scheme) { s.HTTP.Port = 9000; s.Healthz.Port = 9000 },
			"must differ",
		},
		{
			"rate_limit_invalid",
			func(s *Scheme) { s.RateLimit.Enabled = true; s.RateLimit.RequestsPerMinute = 0 },
			"requests_per_minute",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetViper(t)
			cfg := happyPathScheme()
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("expected validation error containing %q, got nil", tc.wantSub)
			}
			if !contains(err.Error(), tc.wantSub) {
				t.Fatalf("expected error to contain %q, got %q", tc.wantSub, err.Error())
			}
		})
	}
}

func TestValidateAcceptsHappyPath(t *testing.T) {
	resetViper(t)
	t.Cleanup(func() { viper.Reset() })

	cfg := happyPathScheme()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected happy-path scheme to validate, got: %v", err)
	}
}

func happyPathScheme() Scheme {
	return Scheme{
		Env: "test",
		HTTP: HTTPConfig{
			Host: "0.0.0.0",
			Port: 8080,
		},
		Healthz: HealthzConfig{Host: "0.0.0.0", Port: 8081},
		Redis:   RedisConfig{Addr: "localhost:6379"},
		GRPCClient: GRPCClientConfig{
			PriceServiceAddr:   "localhost:50051",
			IndexerServiceAddr: "localhost:50052",
			OracleServiceAddr:  "localhost:50053",
		},
		Author: AuthorConfig{Name: "Andrei Solovov"},
		RateLimit: RateLimitConfig{
			Enabled:           true,
			RequestsPerMinute: 60,
			BurstSize:         10,
		},
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
