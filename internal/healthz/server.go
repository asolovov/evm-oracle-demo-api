// Package healthz hosts the operator-facing /healthz, /readyz, /metrics
// endpoints on a dedicated port so they can be firewalled off the public
// load balancer.
package healthz

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// ReadyChecker reports readiness. Returns nil when the service is ready to
// accept traffic, error otherwise.
type ReadyChecker func(ctx context.Context) error

// Server hosts the operator endpoints.
type Server struct {
	cfg     config.HealthzConfig
	srv     *http.Server
	ready   ReadyChecker
	version string
	service string
}

// New constructs the operator listener. metricsReg is mounted at /metrics
// via promhttp.HandlerFor — pass the service-scoped registry, not the
// global default.
func New(cfg config.HealthzConfig, metricsReg *prometheus.Registry, service, version string, ready ReadyChecker) (*Server, error) {
	mux := http.NewServeMux()
	s := &Server{
		cfg:     cfg,
		ready:   ready,
		version: version,
		service: service,
	}
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleReadyz)
	mux.Handle("/metrics", promhttp.HandlerFor(metricsReg, promhttp.HandlerOpts{}))

	s.srv = &http.Server{
		Addr:              net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return s, nil
}

// Serve runs ListenAndServe until the listener returns. ErrServerClosed is
// suppressed.
func (s *Server) Serve() error {
	logger.Log().Infof("healthz/metrics listener starting on %s", s.srv.Addr)
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("healthz ListenAndServe: %w", err)
	}
	return nil
}

// Shutdown stops the listener gracefully.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": s.service,
		"version": s.version,
	})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if s.ready == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "not_ready",
			"error":  err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
