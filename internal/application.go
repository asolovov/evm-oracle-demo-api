// Package internal contains the core application wiring.
package internal

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/module"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
	"github.com/asolovov/evm-oracle-demo-api/pkg/version"
)

// App is the BFF application instance.
type App struct {
	config  *config.Scheme
	version *version.Version
	modules *module.Manager
}

// NewApplication constructs a new App.
func NewApplication() (*App, error) {
	ver, err := version.NewVersion()
	if err != nil {
		return nil, fmt.Errorf("init app version: %w", err)
	}

	return &App{
		config:  &config.Scheme{},
		version: ver,
		modules: module.NewManager(),
	}, nil
}

// Init initialises every registered module. Real wiring lands in later tasks.
func (app *App) Init() error {
	return nil
}

// Serve starts the registered modules and blocks until a shutdown signal arrives.
func (app *App) Serve() error {
	ctx := context.Background()
	if err := app.modules.StartAll(ctx); err != nil {
		return fmt.Errorf("start modules: %w", err)
	}

	logger.Log().Info("application is running, press Ctrl+C to stop")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-quit
	logger.Log().Info("shutdown signal received, stopping gracefully...")

	return nil
}

// Stop runs the module Stop graph with a bounded deadline.
func (app *App) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return app.modules.StopAll(ctx)
}

// Config exposes the config scheme.
func (app *App) Config() *config.Scheme { return app.config }

// Version returns the version string.
func (app *App) Version() string { return app.version.String() }

// Modules exposes the module manager (used by healthchecks).
func (app *App) Modules() *module.Manager { return app.modules }
