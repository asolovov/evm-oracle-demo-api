package wshub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/aggregatorregistry"
	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/metrics"
	"github.com/asolovov/evm-oracle-demo-api/internal/middleware"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

const wethIDHash = "0x0f8a193ff464434486c0daf7db2a895884365d2bc84ba47a68fcf89c1b14b5b8" // keccak256("WETH"), per deployment

// --- mock clients --------------------------------------------------------

type priceMock struct {
	mu       sync.Mutex
	emit     chan models.AggregatedPrice
	finished chan struct{}
}

func (m *priceMock) GetPrice(_ context.Context, _ string) (models.AggregatedPrice, error) {
	return models.AggregatedPrice{}, nil
}

func (m *priceMock) Subscribe(ctx context.Context, _ []string, onMsg func(models.AggregatedPrice)) error {
	m.mu.Lock()
	m.finished = make(chan struct{})
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		close(m.finished)
		m.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case p, ok := <-m.emit:
			if !ok {
				return nil
			}
			onMsg(p)
		}
	}
}

func (m *priceMock) Close() error { return nil }

type indexerMock struct {
	mu       sync.Mutex
	emit     chan models.Event
	finished chan struct{}
}

func (m *indexerMock) ListEvents(_ context.Context, _ indexerclient.ListEventsFilter) ([]models.Event, indexerclient.PageInfo, error) {
	return nil, indexerclient.PageInfo{}, nil
}

func (m *indexerMock) GetRequest(_ context.Context, _ string) (models.RequestSummary, error) {
	return models.RequestSummary{}, indexerclient.ErrNotFound
}

func (m *indexerMock) StreamEvents(ctx context.Context, _ indexerclient.StreamEventsFilter, onMsg func(models.Event)) error {
	m.mu.Lock()
	m.finished = make(chan struct{})
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		close(m.finished)
		m.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case e, ok := <-m.emit:
			if !ok {
				return nil
			}
			onMsg(e)
		}
	}
}

func (m *indexerMock) Close() error { return nil }

// --- helpers -------------------------------------------------------------

func newTestHub(t *testing.T) (*Hub, *priceMock, *indexerMock, *aggregatorregistry.Registry) {
	t.Helper()
	price := &priceMock{emit: make(chan models.AggregatedPrice, 16)}
	indexer := &indexerMock{emit: make(chan models.Event, 16)}
	reg := aggregatorregistry.New()
	hub := NewHub(
		config.GRPCClientConfig{
			Subscribe: config.SubscribeConfig{AssetIDs: []string{"weth"}, ReconnectBackoff: "100ms"},
		},
		price, indexer, reg,
		Options{ClientBufferSize: 4},
	)
	return hub, price, indexer, reg
}

// --- tests ---------------------------------------------------------------

func TestHubBroadcastsToConnectedClients(t *testing.T) {
	hub, price, _, _ := newTestHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(hub.Stop)

	srv := httptest.NewServer(http.HandlerFunc(hub.Serve))
	t.Cleanup(srv.Close)
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1)

	const clients = 3
	conns := make([]*websocket.Conn, clients)
	for i := 0; i < clients; i++ {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("dial #%d: %v", i, err)
		}
		t.Cleanup(func() { _ = c.Close() })
		conns[i] = c
	}

	// Wait until the hub registered every client.
	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() < clients && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.ClientCount() != clients {
		t.Fatalf("expected %d connected clients, got %d", clients, hub.ClientCount())
	}

	price.emit <- models.AggregatedPrice{AssetID: "weth", MedianPrice: 3450.20}

	for i, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, payload, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("client #%d read: %v", i, err)
		}
		var env WireMessage
		if err := json.Unmarshal(payload, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Type != MessageTypePrice {
			t.Fatalf("client #%d expected price envelope, got %q", i, env.Type)
		}
	}
}

// TestHubUpgradeThroughMiddlewareChain reproduces the regression caught
// during the live-stack verify: AccessLog's statusRecorder + the metrics
// middleware's metricsRecorder both wrap http.ResponseWriter, and if either
// fails to delegate Hijack(), gorilla/websocket's upgrade returns 500
// instead of 101. Drives a real WS handshake through the full chi chain.
func TestHubUpgradeThroughMiddlewareChain(t *testing.T) {
	hub, _, _, _ := newTestHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(hub.Stop)

	m := metrics.New(metrics.Options{WSConnectionCount: func() float64 { return float64(hub.ClientCount()) }})

	r := chi.NewRouter()
	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog())
	r.Use(middleware.Recovery())
	r.Use(m.Middleware())
	r.Get("/ws/stream", hub.Serve)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws/stream"
	c, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		var status int
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("ws upgrade through chi+middleware failed: %v (status=%d)", err, status)
	}
	t.Cleanup(func() { _ = c.Close() })

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("expected 101 Switching Protocols, got %d", resp.StatusCode)
	}
}

func TestHubUpdatesRegistryFromAssetRegistered(t *testing.T) {
	hub, _, indexer, reg := newTestHub(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(hub.Stop)

	indexer.emit <- models.Event{
		Kind: models.EventKindAssetRegistered,
		AssetRegistered: &models.AssetRegisteredEvent{
			AssetID:    wethIDHash,
			Aggregator: "0xfeedfacefeedfacefeedfacefeedfacefeedface",
		},
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if addr, ok := reg.Aggregator("weth"); ok && addr != "" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("registry not updated within deadline; snapshot=%v", reg.Snapshot())
}

func TestHubDropsSlowConsumer(t *testing.T) {
	hub, price, _, _ := newTestHub(t)

	// Two clients registered directly so we don't have to dial WS for
	// the slow-consumer drop test. One drains, the other never reads.
	fast := &Client{hub: hub, send: make(chan []byte, 16), closed: make(chan struct{})}
	slow := &Client{hub: hub, send: make(chan []byte, 1), closed: make(chan struct{})}
	hub.register(fast)
	hub.register(slow)

	// Drain fast asynchronously so its channel never fills.
	go func() {
		for range fast.send {
		}
	}()

	// Push way past the slow buffer so we hit the drop branch.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub.Start(ctx)
	t.Cleanup(hub.Stop)

	for i := 0; i < 5; i++ {
		price.emit <- models.AggregatedPrice{AssetID: "weth", MedianPrice: float64(i)}
	}

	deadline := time.Now().Add(2 * time.Second)
	for hub.DropCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if hub.DropCount() == 0 {
		t.Fatalf("expected at least one slow-consumer drop")
	}
}

func TestPriceLoopReconnectsAfterError(t *testing.T) {
	// erroringPriceMock returns immediately with an error on Subscribe.
	// After a brief sleep the loop should retry, which we observe by
	// counting Subscribe invocations.
	hub, _, _, _ := newTestHub(t)
	hub.cfg.Subscribe.ReconnectBackoff = "10ms" // override to keep the test fast.
	erroring := &countingPriceMock{}
	hub.price = erroring

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	hub.Start(ctx)
	t.Cleanup(hub.Stop)

	<-ctx.Done()

	erroring.mu.Lock()
	count := erroring.count
	erroring.mu.Unlock()
	if count < 2 {
		t.Fatalf("expected >= 2 Subscribe attempts after error, got %d", count)
	}
}

type countingPriceMock struct {
	mu    sync.Mutex
	count int
}

func (m *countingPriceMock) GetPrice(_ context.Context, _ string) (models.AggregatedPrice, error) {
	return models.AggregatedPrice{}, nil
}
func (m *countingPriceMock) Subscribe(_ context.Context, _ []string, _ func(models.AggregatedPrice)) error {
	m.mu.Lock()
	m.count++
	m.mu.Unlock()
	return errStreamBroken
}
func (m *countingPriceMock) Close() error { return nil }

var errStreamBroken = stringErr("stream broken")

type stringErr string

func (s stringErr) Error() string { return string(s) }

func TestSleepWithCtxAbortsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	sleepWithCtx(ctx, time.Hour)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("sleepWithCtx should return immediately on canceled ctx")
	}
}

func TestMarshalEnvelopes(t *testing.T) {
	payload, err := MarshalPrice(models.AggregatedPrice{AssetID: "weth", MedianPrice: 100})
	if err != nil {
		t.Fatalf("MarshalPrice: %v", err)
	}
	var env WireMessage
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != MessageTypePrice {
		t.Fatalf("price envelope type = %q", env.Type)
	}

	payload, err = MarshalEvent(models.Event{Kind: models.EventKindPriceRequested})
	if err != nil {
		t.Fatalf("MarshalEvent: %v", err)
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Type != MessageTypeEvent {
		t.Fatalf("event envelope type = %q", env.Type)
	}
}
