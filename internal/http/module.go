// Package http hosts the REST + WebSocket transport. The implementation is
// fleshed out across subsequent commits (handlers, middlewares, WS hub). This
// skeleton keeps the module manager wiring stable in the meantime.
package http

import (
	"context"

	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// Module is the http transport module placeholder.
type Module struct{}

// NewModule returns an inert module that does nothing until later commits flesh it out.
func NewModule() *Module { return &Module{} }

// Name returns the module identifier.
func (m *Module) Name() string { return "http" }

// Init does nothing yet.
func (m *Module) Init(_ context.Context) error {
	logger.Log().Infof("initializing %s module (placeholder)", m.Name())
	return nil
}

// Start does nothing yet.
func (m *Module) Start(_ context.Context) error { return nil }

// Stop does nothing yet.
func (m *Module) Stop(_ context.Context) error { return nil }

// HealthCheck reports healthy until the real server lands.
func (m *Module) HealthCheck(_ context.Context) error { return nil }
