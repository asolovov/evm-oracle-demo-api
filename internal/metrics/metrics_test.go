package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func TestMetricsRegistryAndMiddleware(t *testing.T) {
	m := New(Options{WSConnectionCount: func() float64 { return 3 }})

	// Exercise the http middleware on a chi-routed mux so RoutePattern
	// returns a stable label.
	r := chi.NewRouter()
	r.Use(m.Middleware())
	r.Get("/api/v1/assets/{id}/price", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/weth/price", nil))
	}

	// Bump the auxiliary counters directly so scraping below sees them.
	m.WSMessagesSentTotal.Add(7)
	m.WSDropsTotal.Add(1)
	m.RateLimitRejectedTotal.WithLabelValues("any").Add(2)
	m.UpstreamGRPCCallsTotal.WithLabelValues("price.v1", "GetPrice", "OK").Inc()

	// Scrape via promhttp and assert each metric series is present.
	srv := httptest.NewServer(promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{}))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	text := string(body)

	want := []string{
		// Route-template label, not the raw URL.
		`http_requests_total{method="GET",path="/api/v1/assets/{id}/price",status="200"} 5`,
		`ws_connections_active 3`,
		`ws_messages_sent_total 7`,
		`ws_drops_total 1`,
		`ratelimit_rejected_total{ip_class="any"} 2`,
		`upstream_grpc_calls_total{method="GetPrice",service="price.v1",status="OK"} 1`,
	}
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("expected scrape body to contain %q\n--- body ---\n%s", w, text)
		}
	}
}
