// Package config defines application configuration defaults and schema.
package config

import (
	"github.com/spf13/viper"
)

//nolint:gochecknoinits // configuration defaults are registered at package load.
func init() {
	setDefaults()
}

// setDefaults registers every nested viper key the BFF reads. Per architecture
// rule 6 viper.AutomaticEnv alone does not populate nested keys on Unmarshal,
// so every key must be present in viper's namespace before load — the
// SetDefault call site doubles as machine-discoverable documentation
// (`grep "SetDefault" config/` enumerates the entire env-var surface).
func setDefaults() {
	viper.SetDefault("env", "prod")

	// HTTP listener (public REST + WS surface).
	viper.SetDefault("http.host", "0.0.0.0")
	viper.SetDefault("http.port", 8080)
	viper.SetDefault("http.read_timeout", "15s")
	viper.SetDefault("http.write_timeout", "15s")
	viper.SetDefault("http.idle_timeout", "60s")
	viper.SetDefault("http.cors_origins", []string{})
	viper.SetDefault("http.cors_allow_all", false)
	viper.SetDefault("http.trusted_proxies", []string{})

	// Healthz listener (operator-facing — /healthz + /readyz + /metrics).
	viper.SetDefault("healthz.host", "0.0.0.0")
	viper.SetDefault("healthz.port", 8081)

	// Redis (rate limit state + hot-path caches).
	viper.SetDefault("redis.addr", "localhost:6379")
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	// gRPC client dials for upstream services.
	viper.SetDefault("grpc_client.price_service_addr", "localhost:50051")
	viper.SetDefault("grpc_client.indexer_service_addr", "localhost:50052")
	viper.SetDefault("grpc_client.dial_timeout", "10s")
	viper.SetDefault("grpc_client.use_tls", false)
	viper.SetDefault("grpc_client.keep_alive.time", "30s")
	viper.SetDefault("grpc_client.keep_alive.timeout", "10s")
	viper.SetDefault("grpc_client.keep_alive.permit_without_stream", true)
	viper.SetDefault("grpc_client.subscribe.asset_ids", []string{})
	viper.SetDefault("grpc_client.subscribe.reconnect_backoff", "5s")

	// Rate limit defaults match the spec NFR-08 / acceptance criteria.
	viper.SetDefault("rate_limit.enabled", true)
	viper.SetDefault("rate_limit.requests_per_minute", 60)
	viper.SetDefault("rate_limit.burst_size", 10)

	// Credential surface (FR-09). Operators must set AUTHOR_NAME at deploy
	// time; the rest are optional links rendered next to the author block.
	viper.SetDefault("author.name", "")
	viper.SetDefault("author.links", map[string]string{})

	// Chain defaults match the live deployment on Ethereum Sepolia
	// (chainId 11155111, registry contract from evm-oracle-demo-contracts
	// main/deployments/ethereum-sepolia/). Override via env to retarget.
	viper.SetDefault("chain.chain_id", int64(11155111))
	viper.SetDefault("chain.name", "ethereum-sepolia")
	viper.SetDefault("chain.registry_address", "0x89a6c12a403733c6a817472cec46a530581cb7ef")
	viper.SetDefault("chain.explorer_url_pattern", "https://sepolia.etherscan.io/tx/{tx_hash}")

	// Telemetry.
	viper.SetDefault("telemetry.log_level", "info")
	viper.SetDefault("telemetry.log_format", "json")
}
