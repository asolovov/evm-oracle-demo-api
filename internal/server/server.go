// Package server owns the chi router + http.Server lifecycle for the BFF.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/handlers"
	"github.com/asolovov/evm-oracle-demo-api/internal/middleware"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// Server is the public REST + WebSocket listener. WebSocket hookup lands in
// a later commit; this commit wires the REST surface.
type Server struct {
	cfg    config.HTTPConfig
	srv    *http.Server
	router *chi.Mux
}

// New constructs a Server with the API mounted under /api/v1. apiMiddleware
// is applied to the /api/v1 sub-route only — the WebSocket route bypasses
// it (slow WS connections are fine; the rate limit is for REST endpoints).
func New(cfg config.HTTPConfig, api *handlers.API, apiMiddleware ...func(http.Handler) http.Handler) (*Server, error) {
	read, err := time.ParseDuration(cfg.ReadTimeout)
	if err != nil {
		return nil, fmt.Errorf("parse http.read_timeout: %w", err)
	}
	write, err := time.ParseDuration(cfg.WriteTimeout)
	if err != nil {
		return nil, fmt.Errorf("parse http.write_timeout: %w", err)
	}
	idle, err := time.ParseDuration(cfg.IdleTimeout)
	if err != nil {
		return nil, fmt.Errorf("parse http.idle_timeout: %w", err)
	}

	router := chi.NewRouter()
	api.Register(router, middleware.CORS(cfg), apiMiddleware...)

	srv := &http.Server{
		Addr:         net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:      router,
		ReadTimeout:  read,
		WriteTimeout: write,
		IdleTimeout:  idle,
	}

	return &Server{cfg: cfg, srv: srv, router: router}, nil
}

// Router returns the underlying chi mux. The hub mounts /ws/stream onto it.
func (s *Server) Router() chi.Router { return s.router }

// HandleWebSocket registers a handler under /ws/stream on the server's
// router. Called by application.go after the hub is constructed.
func (s *Server) HandleWebSocket(h http.HandlerFunc) {
	s.router.Get("/ws/stream", h)
}

// Serve blocks until the listener errors. http.ErrServerClosed is the
// expected shutdown signal and is suppressed.
func (s *Server) Serve() error {
	logger.Log().Infof("REST + WS listener starting on %s", s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http.ListenAndServe: %w", err)
	}
	return nil
}

// Shutdown stops the listener gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}
