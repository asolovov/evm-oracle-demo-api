// Package metrics owns the BFF's service-scoped Prometheus registry +
// helper instruments. Constructed once in application.go; surfaced via the
// dedicated healthz HTTP server at /metrics.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics bundles every Prometheus collector the BFF emits.
type Metrics struct {
	Registry *prometheus.Registry

	HTTPRequestsTotal      *prometheus.CounterVec
	HTTPRequestDurationS   *prometheus.HistogramVec
	WSConnectionsActive    prometheus.GaugeFunc
	WSMessagesSentTotal    prometheus.Counter
	WSDropsTotal           prometheus.Counter
	RateLimitRejectedTotal *prometheus.CounterVec
	UpstreamGRPCCallsTotal *prometheus.CounterVec
}

// Options carries dependencies the metrics package can't infer from config.
type Options struct {
	// WSConnectionCount is invoked by the WSConnectionsActive gauge on
	// scrape — the hub provides this via its ClientCount method.
	WSConnectionCount func() float64
}

// New constructs and registers every collector. Panics on duplicate
// registration so misconfiguration surfaces loud.
func New(opts Options) *Metrics {
	reg := prometheus.NewRegistry()

	httpReq := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests processed, partitioned by method, path, and status.",
	}, []string{"method", "path", "status"})

	httpDur := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request duration histogram, partitioned by method and path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	wsConn := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "ws_connections_active",
		Help: "Currently-connected WebSocket clients.",
	}, func() float64 {
		if opts.WSConnectionCount != nil {
			return opts.WSConnectionCount()
		}
		return 0
	})

	wsMsg := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_messages_sent_total",
		Help: "Total WebSocket frames fanned out to connected clients.",
	})

	wsDrops := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "ws_drops_total",
		Help: "Total slow-consumer drops at the WebSocket hub.",
	})

	rlRej := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ratelimit_rejected_total",
		Help: "Total rate-limit rejections, partitioned by ip_class.",
	}, []string{"ip_class"})

	grpcCalls := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "upstream_grpc_calls_total",
		Help: "Total upstream gRPC calls issued by the BFF, partitioned by service, method, and grpc status code.",
	}, []string{"service", "method", "status"})

	reg.MustRegister(
		httpReq, httpDur, wsConn, wsMsg, wsDrops, rlRej, grpcCalls,
	)

	return &Metrics{
		Registry:               reg,
		HTTPRequestsTotal:      httpReq,
		HTTPRequestDurationS:   httpDur,
		WSConnectionsActive:    wsConn,
		WSMessagesSentTotal:    wsMsg,
		WSDropsTotal:           wsDrops,
		RateLimitRejectedTotal: rlRej,
		UpstreamGRPCCallsTotal: grpcCalls,
	}
}
