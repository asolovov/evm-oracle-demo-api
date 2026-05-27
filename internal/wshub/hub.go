package wshub

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/aggregatorregistry"
	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
	"github.com/asolovov/evm-oracle-demo-api/internal/priceclient"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// Hub is the in-memory pub/sub backing the /ws/stream endpoint. One Hub per
// process; clients register / unregister as they connect / disconnect. The
// hub itself drives the two upstream subscriptions in background goroutines.
type Hub struct {
	cfg      config.GRPCClientConfig
	price    priceclient.Client
	indexer  indexerclient.Client
	registry *aggregatorregistry.Registry

	clientBufferSize int

	mu      sync.RWMutex
	clients map[*Client]struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup

	dropMu sync.Mutex
	drops  uint64

	onSend func()
	onDrop func()
}

// Options carries the construction parameters that can't be derived from
// config alone.
type Options struct {
	ClientBufferSize int
	// OnSend fires once per fan-out broadcast (one increment per frame
	// delivered to any client). Used by the metrics package.
	OnSend func()
	// OnDrop fires once per slow-consumer drop.
	OnDrop func()
}

// NewHub constructs an unstarted Hub.
func NewHub(
	cfg config.GRPCClientConfig,
	price priceclient.Client,
	indexer indexerclient.Client,
	registry *aggregatorregistry.Registry,
	opts Options,
) *Hub {
	buf := opts.ClientBufferSize
	if buf <= 0 {
		buf = 256
	}
	return &Hub{
		cfg:              cfg,
		price:            price,
		indexer:          indexer,
		registry:         registry,
		clientBufferSize: buf,
		clients:          make(map[*Client]struct{}),
		onSend:           opts.OnSend,
		onDrop:           opts.OnDrop,
	}
}

// Start kicks off the upstream subscription loops. Idempotent.
func (h *Hub) Start(ctx context.Context) {
	if h.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	h.cancel = cancel

	h.wg.Add(2)
	go func() {
		defer h.wg.Done()
		h.runPriceSubscribe(ctx)
	}()
	go func() {
		defer h.wg.Done()
		h.runIndexerStream(ctx)
	}()
}

// Stop tears down the upstream loops and closes every connected client.
// Safe to call multiple times.
func (h *Hub) Stop() {
	if h.cancel == nil {
		return
	}
	h.cancel()
	h.cancel = nil

	// Snapshot under the read lock so close() can call unregister
	// without deadlocking on the write lock.
	h.mu.RLock()
	snapshot := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		snapshot = append(snapshot, c)
	}
	h.mu.RUnlock()
	for _, c := range snapshot {
		c.close()
	}

	h.wg.Wait()
}

// ClientCount returns the current number of connected WS clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// DropCount returns the running total of slow-consumer drops since Start.
func (h *Hub) DropCount() uint64 {
	h.dropMu.Lock()
	defer h.dropMu.Unlock()
	return h.drops
}

// register adds a client to the fan-out set.
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

// unregister removes a client from the fan-out set. Idempotent.
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
}

// broadcast fans the payload out to every connected client. Slow consumers
// are dropped (channel full) and the running drop counter is incremented so
// the metrics package can surface it.
func (h *Hub) broadcast(payload []byte) {
	h.mu.RLock()
	snapshot := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		snapshot = append(snapshot, c)
	}
	h.mu.RUnlock()

	for _, c := range snapshot {
		select {
		case c.send <- payload:
			if h.onSend != nil {
				h.onSend()
			}
		default:
			h.dropMu.Lock()
			h.drops++
			h.dropMu.Unlock()
			if h.onDrop != nil {
				h.onDrop()
			}
			c.close()
		}
	}
}

// runPriceSubscribe drives price.Subscribe with reconnect-on-error.
func (h *Hub) runPriceSubscribe(ctx context.Context) {
	backoff, err := time.ParseDuration(h.cfg.Subscribe.ReconnectBackoff)
	if err != nil || backoff <= 0 {
		backoff = 5 * time.Second
	}

	assetIDs := h.cfg.Subscribe.AssetIDs
	if len(assetIDs) == 0 {
		assetIDs = models.AssetIDs()
	}

	for ctx.Err() == nil {
		err := h.price.Subscribe(ctx, assetIDs, func(p models.AggregatedPrice) {
			payload, err := MarshalPrice(p)
			if err != nil {
				logger.Log().WithError(err).Warn("wshub: marshal price payload failed")
				return
			}
			h.broadcast(payload)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Log().WithError(err).Warn("wshub: price.Subscribe ended; reconnecting after backoff")
		}
		if ctx.Err() != nil {
			return
		}
		sleepWithCtx(ctx, backoff)
	}
}

// runIndexerStream drives indexer.StreamEvents with reconnect-on-error.
func (h *Hub) runIndexerStream(ctx context.Context) {
	backoff, err := time.ParseDuration(h.cfg.Subscribe.ReconnectBackoff)
	if err != nil || backoff <= 0 {
		backoff = 5 * time.Second
	}

	for ctx.Err() == nil {
		err := h.indexer.StreamEvents(ctx, indexerclient.StreamEventsFilter{}, func(e models.Event) {
			if e.Kind == models.EventKindAssetRegistered && e.AssetRegistered != nil {
				if id, ok := aggregatorregistry.AssetIDFromBytes32Hex(e.AssetRegistered.AssetID); ok {
					h.registry.Set(id, e.AssetRegistered.Aggregator)
				}
			}
			payload, err := MarshalEvent(e)
			if err != nil {
				logger.Log().WithError(err).Warn("wshub: marshal event payload failed")
				return
			}
			h.broadcast(payload)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Log().WithError(err).Warn("wshub: indexer.StreamEvents ended; reconnecting after backoff")
		}
		if ctx.Err() != nil {
			return
		}
		sleepWithCtx(ctx, backoff)
	}
}

func sleepWithCtx(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
