// Package internal contains the core application wiring.
package internal

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/aggregatorregistry"
	"github.com/asolovov/evm-oracle-demo-api/internal/handlers"
	"github.com/asolovov/evm-oracle-demo-api/internal/healthz"
	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/metrics"
	"github.com/asolovov/evm-oracle-demo-api/internal/module"
	"github.com/asolovov/evm-oracle-demo-api/internal/priceclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/ratelimit"
	"github.com/asolovov/evm-oracle-demo-api/internal/server"
	"github.com/asolovov/evm-oracle-demo-api/internal/wshub"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
	"github.com/asolovov/evm-oracle-demo-api/pkg/version"
)

// App is the BFF application instance. Per architecture rules 1+2 every
// component is constructed and wired here; no module reaches out to others.
type App struct {
	config  *config.Scheme
	version *version.Version
	modules *module.Manager

	priceClient   priceclient.Client
	indexerClient indexerclient.Client
	registry      *aggregatorregistry.Registry
	hub           *wshub.Hub
	server        *server.Server
	healthz       *healthz.Server
	redisClient   redis.UniversalClient
	metrics       *metrics.Metrics

	wg      sync.WaitGroup
	stopped sync.Once
}

// NewApplication constructs an empty App. Wiring happens in Init.
func NewApplication() (*App, error) {
	ver, err := version.NewVersion()
	if err != nil {
		return nil, fmt.Errorf("init app version: %w", err)
	}

	return &App{
		config:   &config.Scheme{},
		version:  ver,
		modules:  module.NewManager(),
		registry: aggregatorregistry.New(),
	}, nil
}

// Init validates config, dials upstream services, seeds the aggregator
// registry, and constructs the HTTP server.
func (app *App) Init() error {
	if err := app.config.Validate(); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	pc, err := priceclient.Dial(app.config.GRPCClient)
	if err != nil {
		return fmt.Errorf("price client: %w", err)
	}
	app.priceClient = pc

	ix, err := indexerclient.Dial(app.config.GRPCClient)
	if err != nil {
		// Tear down the price client before bubbling the error so a
		// later restart isn't holding a dangling conn.
		_ = pc.Close()
		return fmt.Errorf("indexer client: %w", err)
	}
	app.indexerClient = ix

	// Best-effort registry seed. The BFF must come up even if the
	// indexer is temporarily unreachable — build-tx returns 503 until the
	// registry has the aggregator address. The WS hub will refresh on
	// live AssetRegistered events in a later commit.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.registry.Load(ctx, ix); err != nil {
		logger.Log().WithError(err).Warn("aggregator registry: initial load failed (will retry on first live event)")
	}

	// Metrics — constructed before the hub so the hub's OnSend / OnDrop
	// callbacks can reference the counters. WSConnectionCount is wired
	// post-hub-construction below.
	app.metrics = metrics.New(metrics.Options{
		WSConnectionCount: func() float64 { return 0 },
	})

	api := &handlers.API{
		Price:            app.priceClient,
		Indexer:          app.indexerClient,
		Registry:         app.registry,
		Author:           app.config.Author,
		Chain:            app.config.Chain,
		Version:          app.Version(),
		ServiceID:        "evm-oracle-demo-api",
		GlobalMiddleware: []func(http.Handler) http.Handler{app.metrics.Middleware()},
	}

	app.redisClient = redis.NewClient(&redis.Options{
		Addr:     app.config.Redis.Addr,
		Password: app.config.Redis.Password,
		DB:       app.config.Redis.DB,
	})

	var apiMW []func(http.Handler) http.Handler
	if app.config.RateLimit.Enabled {
		limiter := ratelimit.NewRedisLimiter(app.redisClient, app.config.RateLimit.RequestsPerMinute, app.config.RateLimit.BurstSize)
		// /api/v1/health is operator-facing and never rate-limited so a
		// crashed Redis can't blackhole readiness checks.
		apiMW = append(apiMW, ratelimit.Middleware(
			limiter,
			app.config.HTTP.TrustedProxies,
			// Health is operator-facing; docs + the OpenAPI spec are
			// static metadata. Rate-limiting any of these would be
			// gratuitous.
			[]string{"/api/v1/health", "/api/v1/docs", "/api/v1/openapi.yaml"},
			func(ipClass string) { app.metrics.RateLimitRejectedTotal.WithLabelValues(ipClass).Inc() },
		))
	}

	srv, err := server.New(app.config.HTTP, api, apiMW...)
	if err != nil {
		_ = pc.Close()
		_ = ix.Close()
		_ = app.redisClient.Close()
		return fmt.Errorf("http server: %w", err)
	}
	app.server = srv

	app.hub = wshub.NewHub(
		app.config.GRPCClient,
		app.priceClient,
		app.indexerClient,
		app.registry,
		wshub.Options{
			OnSend: func() { app.metrics.WSMessagesSentTotal.Inc() },
			OnDrop: func() { app.metrics.WSDropsTotal.Inc() },
		},
	)
	app.server.HandleWebSocket(app.hub.Serve)

	// Re-construct the metrics layer with a real ws_connections_active
	// gauge function now that we have the hub instance. The previous
	// `metrics.New(...)` was a bootstrap with a zero gauge — its
	// counters are still in use, so we keep the same registry and only
	// rewire the gauge by replacing the struct.
	hub := app.hub
	app.metrics = metrics.New(metrics.Options{
		WSConnectionCount: func() float64 { return float64(hub.ClientCount()) },
	})

	hz, err := healthz.New(
		app.config.Healthz,
		app.metrics.Registry,
		"evm-oracle-demo-api",
		app.Version(),
		nil,
	)
	if err != nil {
		_ = pc.Close()
		_ = ix.Close()
		_ = app.redisClient.Close()
		return fmt.Errorf("healthz server: %w", err)
	}
	app.healthz = hz

	logger.Log().Info("application initialized")
	return nil
}

// Serve runs the listener in a background goroutine and blocks until a
// shutdown signal arrives.
func (app *App) Serve() error {
	if app.server == nil {
		return errors.New("Serve called before Init")
	}

	// Hub goroutines run independently of the http.Server lifecycle —
	// Stop() drains them via Hub.Stop after the listener shuts down.
	app.hub.Start(context.Background())

	app.wg.Add(1)
	errCh := make(chan error, 1)
	go func() {
		defer app.wg.Done()
		if err := app.server.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		if err := app.healthz.Serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	logger.Log().Info("application is running, press Ctrl+C to stop")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	select {
	case <-quit:
		logger.Log().Info("shutdown signal received, stopping gracefully...")
	case err := <-errCh:
		return fmt.Errorf("http listener: %w", err)
	}
	return nil
}

// Stop tears down listeners + upstream clients with a bounded deadline.
//
//nolint:gocognit // Shutdown intentionally walks every owned resource and
// records the first error; flattening helpers would obscure the order.
func (app *App) Stop() error {
	var firstErr error
	app.stopped.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if app.server != nil {
			if err := app.server.Shutdown(ctx); err != nil {
				logger.Log().WithError(err).Warn("server shutdown error")
				firstErr = err
			}
		}
		if app.healthz != nil {
			if err := app.healthz.Shutdown(ctx); err != nil && firstErr == nil {
				logger.Log().WithError(err).Warn("healthz shutdown error")
				firstErr = err
			}
		}
		if app.hub != nil {
			app.hub.Stop()
		}
		app.wg.Wait()

		if app.priceClient != nil {
			if err := app.priceClient.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if app.indexerClient != nil {
			if err := app.indexerClient.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if app.redisClient != nil {
			if err := app.redisClient.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if err := app.modules.StopAll(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	})
	return firstErr
}

// Config exposes the config scheme.
func (app *App) Config() *config.Scheme { return app.config }

// Version returns the version string.
func (app *App) Version() string { return app.version.String() }

// Modules exposes the module manager (used by healthchecks).
func (app *App) Modules() *module.Manager { return app.modules }
