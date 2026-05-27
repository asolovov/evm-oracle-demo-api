# evm-oracle-demo-api

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/asolovov/evm-oracle-demo-api.svg)](https://pkg.go.dev/github.com/asolovov/evm-oracle-demo-api)

REST + WebSocket BFF over the [EVM Oracle Demo](https://github.com/asolovov)
gRPC plane. Translates the `price-service` (`price.v1.PriceService`)
and `indexer-service` (`indexer.v1.IndexerService`) into a dashboard-
friendly HTTP surface, multiplexes both upstream streams onto a
single `/ws/stream` WebSocket, and rate-limits public traffic via
Redis.

```
                              ┌─────────────────────┐
                              │  evm-oracle-demo-   │  /api/v1/*  + /ws/stream
   wagmi / browser  ─────────▶│  api  (this repo)   │─────────────────────────▶
                              └─────────┬───────────┘
                                        │ gRPC
                            ┌───────────┴───────────┐
                            │                       │
                     price.v1.Subscribe   indexer.v1.StreamEvents
                            │                       │
                  ┌─────────▼──────┐      ┌─────────▼──────────┐
                  │ price-service  │      │ indexer-service    │
                  └────────────────┘      └────────────────────┘
```

---

## Status

| Concern | Status |
|---------|--------|
| REST surface (`/api/v1/*`) | implemented |
| WebSocket fan-out (`/ws/stream`) | implemented |
| Redis-backed sliding-window rate limit (60/min + burst 10 by default) | implemented |
| Prometheus `/metrics` + `/healthz` + `/readyz` | implemented |
| `requestPrice(bytes32)` calldata builder | implemented |
| Historical price endpoint | **stubbed — 501** until `price-service` exposes a history RPC |
| External audit | out of scope — see the parent [EVM Oracle Demo](https://github.com/asolovov) project |

This is the BFF tier of a portfolio-grade EVM Oracle Demo — see the
parent project's architecture diagram for the full picture. Not for
production use as-is.

---

## Run locally

```bash
make proto-install     # installs pinned buf + protoc-gen-go(-grpc)
make build             # generates protos under internal/genproto/ and builds the binary

# Bring up Redis + the BFF via docker compose:
make compose-up

# Or run the binary against an externally-managed Redis / upstreams:
AUTHOR_NAME="Your Name" \
GRPC_CLIENT_PRICE_SERVICE_ADDR=localhost:50051 \
GRPC_CLIENT_INDEXER_SERVICE_ADDR=localhost:50052 \
./evm-oracle-demo-api serve
```

`make test` runs the full suite (the rate-limit path uses
[miniredis](https://github.com/alicebob/miniredis) so Docker isn't
required for tests). `make test-coverage` opens the per-package
breakdown.

---

## REST contract

All responses are JSON. Error responses share the same envelope:

```json
{ "code": "asset_not_tracked", "message": "asset is not tracked" }
```

| Method | Path                              | Notes                                                                                  |
|--------|-----------------------------------|----------------------------------------------------------------------------------------|
| GET    | `/api/v1/health`                  | Liveness + author credential block (FR-09). Never rate-limited.                        |
| GET    | `/api/v1/assets`                  | 10 catalog entries + per-asset latest price + last on-chain fulfilment.                |
| GET    | `/api/v1/assets/{id}/price`       | Drill-down. 404 on unknown id / no price yet; 502 if `price-service` is unreachable.   |
| GET    | `/api/v1/assets/{id}/history`     | **501** in v1 — `price-service` has no history RPC yet. See *Known gaps*.              |
| GET    | `/api/v1/requests/{reqId}`        | Joined request lifecycle from `indexer.GetRequest`. `req_id` must be a base-10 uint256.|
| POST   | `/api/v1/requests/build-tx`       | ABI-encoded `requestPrice(bytes32)` calldata + resolved aggregator address.            |

### `POST /api/v1/requests/build-tx`

```json
{ "asset_id": "weth", "chain_id": 11155111 }
```

Returns:

```json
{
  "to":          "0xc0Ff…",       // PriceAggregator for this asset
  "data":        "0x….…",         // selector + bytes32 assetId
  "value":       "0",             // wei; v1 doesn't query the contract's requestFee()
  "chain_id":    11155111,
  "chain_name":  "ethereum-sepolia"
}
```

- The endpoint **never submits** — the frontend signs and broadcasts.
- 404 on unknown asset.
- 400 on `chain_id` mismatch.
- 503 when the aggregator address for the requested asset hasn't been
  observed yet (registry seeded from `indexer.ListEvents(ASSET_REGISTERED)`
  on startup; topped up live by the WS hub).

---

## WebSocket contract

Single endpoint: `GET /ws/stream`. Connect, receive a typed JSON
envelope per frame:

```json
{ "type": "price", "payload": { /* AggregatedPrice */ } }
{ "type": "event", "payload": { /* Event */          } }
```

- The hub subscribes to `price.Subscribe` across the 10-asset catalog
  and `indexer.StreamEvents` across all kinds.
- Slow consumers are dropped on backpressure (channel full ⇒ close);
  the drop is counted at `ws_drops_total`.
- The server pings every 30 s and closes idle clients after 60 s.

---

## Configuration

Every key is registered via `viper.SetDefault` in `config/init.go` and
read as the equivalent ENV var with `.` → `_`. A handful of keys are
required (`Validate()` fails fast on startup):

| ENV var                                  | Default                          | Notes |
|------------------------------------------|----------------------------------|-------|
| `HTTP_HOST`                              | `0.0.0.0`                        | Public listener bind host |
| `HTTP_PORT`                              | `8080`                           | REST + WS port |
| `HTTP_CORS_ORIGINS`                      | `[]`                             | Comma-separated origins; empty + `HTTP_CORS_ALLOW_ALL=true` means dev mode. |
| `HTTP_CORS_ALLOW_ALL`                    | `false`                          | If true, echoes any Origin (dev only). |
| `HTTP_TRUSTED_PROXIES`                   | `[]`                             | IPs / CIDRs whose X-Forwarded-For is honored. |
| `HEALTHZ_HOST`                           | `0.0.0.0`                        | Operator listener bind host |
| `HEALTHZ_PORT`                           | `8081`                           | `/healthz`, `/readyz`, `/metrics` |
| `REDIS_ADDR`                             | `localhost:6379`                 | Rate-limit + hot-path cache. **Required.** |
| `REDIS_PASSWORD`                         | (empty)                          |  |
| `REDIS_DB`                               | `0`                              |  |
| `GRPC_CLIENT_PRICE_SERVICE_ADDR`         | `localhost:50051`                | **Required.** |
| `GRPC_CLIENT_INDEXER_SERVICE_ADDR`       | `localhost:50052`                | **Required.** |
| `GRPC_CLIENT_USE_TLS`                    | `false`                          |  |
| `GRPC_CLIENT_KEEP_ALIVE_TIME`            | `30s`                            |  |
| `GRPC_CLIENT_KEEP_ALIVE_TIMEOUT`         | `10s`                            |  |
| `GRPC_CLIENT_SUBSCRIBE_ASSET_IDS`        | (catalog defaults)               | JSON array overriding the WS hub's price.Subscribe set. |
| `GRPC_CLIENT_SUBSCRIBE_RECONNECT_BACKOFF`| `5s`                             | After an upstream stream errors out. |
| `RATE_LIMIT_ENABLED`                     | `true`                           |  |
| `RATE_LIMIT_REQUESTS_PER_MINUTE`         | `60`                             |  |
| `RATE_LIMIT_BURST_SIZE`                  | `10`                             |  |
| `AUTHOR_NAME`                            | (empty)                          | **Required.** Echoed in `/api/v1/health` (FR-09). |
| `AUTHOR_LINKS`                           | `{}`                             | JSON map (e.g. `{"github":"…","linkedin":"…"}`). |
| `CHAIN_CHAIN_ID`                         | `11155111` (Ethereum Sepolia)    | Must match the contracts deployment. |
| `CHAIN_REGISTRY_ADDRESS`                 | `0x89a6c12a403733c6a817472cec46a530581cb7ef` | OracleRegistry contract address. |
| `TELEMETRY_LOG_LEVEL`                    | `info`                           |  |
| `TELEMETRY_LOG_FORMAT`                   | `json`                           |  |

---

## Architecture rule deviations

This service deliberately deviates from one architecture rule shared
across the `evm-oracle-demo-*` Go services:

- **Rule 7 (one service = one database) — deviated.** The BFF has no
  relational store. It is read-only, derives every response from the
  two upstream gRPC services, and uses Redis only for rate-limit + a
  hot-path cache. The deviation is intentional — adding a Postgres
  schema purely to satisfy the rule would be cargo-cult. Documented
  here so reviewers don't have to dig.

All other rules (1-6, 8-9) are honored. In particular:

- `cmd/` only initialises Cobra + config (rule 1).
- `internal/application.go` owns all wiring (rule 2).
- All domain models live in `internal/models/`; conversions are
  methods on the model types (rule 3).
- Internal modules cover only storages, services, servers, and
  in-project handlers (rule 4).
- External gRPC clients (`internal/priceclient`, `internal/indexerclient`)
  are plain packages, not template modules (rule 5).
- All config lives in `/config`, every key has a `viper.SetDefault`
  (rule 6).
- No `cmd/seed`, no fixture files — the few bootstrap-style operations
  (registry seed from indexer events) happen in-process via
  `application.Init` (rule 8).
- `internal/genproto/` is gitignored; the Dockerfile + Makefile
  re-generate from `protocols/` on every build (rule 9).

---

## Metrics

Scrape `http://<host>:8081/metrics`. The service exposes:

| Metric                                  | Type         | Labels                       |
|-----------------------------------------|--------------|------------------------------|
| `http_requests_total`                   | counter      | `method`, `path`, `status`   |
| `http_request_duration_seconds`         | histogram    | `method`, `path`             |
| `ws_connections_active`                 | gauge        | —                            |
| `ws_messages_sent_total`                | counter      | —                            |
| `ws_drops_total`                        | counter      | —                            |
| `ratelimit_rejected_total`              | counter      | `ip_class`                   |
| `upstream_grpc_calls_total`             | counter      | `service`, `method`, `status`|

`path` is the chi route template (e.g. `/api/v1/assets/{id}/price`),
never the raw URL — keeps label cardinality bounded.

---

## Known gaps

| Gap | Plan |
|-----|------|
| `/api/v1/assets/{id}/history` returns **501** | Waits on a `price.v1.PriceService.GetHistory` RPC. The handler shape is in place so it'll wire in one commit once the upstream lands. |
| `build-tx.value` is hardcoded to `"0"` | The aggregator's `requestFee()` view isn't queried — the BFF has no chain client by design. A future enhancement could either dial chain RPC or cache the fee from a periodic indexer-side broadcast. |
| Aggregator registry only resyncs on live `AssetRegistered` events | Acceptable for the demo where assets are seeded once at deploy time. A periodic ListEvents poll could be added but is YAGNI for now. |
| Stream-loop reconnect backoff is fixed | Exponential backoff would be nice; constant `5s` is fine for the demo. |

---

## License

MIT — see [LICENSE](LICENSE).

---

### Built by

**Andrei Solovov** — Senior Blockchain Engineer
[GitHub](https://github.com/asolovov) · [LinkedIn](https://www.linkedin.com/in/asolovov/) · [Upwork](https://www.upwork.com/)

Part of the [EVM Oracle Demo](https://github.com/asolovov) portfolio
project — multi-source pull oracle with on-chain Chainlink-compatible
aggregators, off-chain median + deviation guard, M-of-N reporter
signing, and a Next.js dashboard.
