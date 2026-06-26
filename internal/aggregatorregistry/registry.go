// Package aggregatorregistry caches the asset_id -> aggregator-address map
// the build-tx helper needs to resolve a user-friendly asset id ("weth")
// into the actual PriceAggregator contract a `requestPrice` tx should hit.
//
// The cache is seeded at startup from indexer.ListEvents(ASSET_REGISTERED)
// and kept fresh by the WS hub forwarding live AssetRegistered events
// through Set. Aggregator addresses don't change during the lifetime of a
// deployment, so a single ListEvents at startup is enough — the live tail
// only matters when new assets are registered after the BFF is up.
package aggregatorregistry

import (
	"context"
	"fmt"
	"sync"

	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// Registry maps the lowercase-symbol asset id form ("weth") onto the
// 0x-prefixed aggregator contract address.
type Registry struct {
	mu      sync.RWMutex
	byAsset map[string]string
}

// New returns an empty registry. Call Load to seed from the indexer.
func New() *Registry {
	return &Registry{byAsset: make(map[string]string)}
}

// Load issues one indexer.ListEvents(ASSET_REGISTERED) call and populates the
// cache. Safe to call multiple times — subsequent loads merge.
func (r *Registry) Load(ctx context.Context, ix indexerclient.Client) error {
	if ix == nil {
		return fmt.Errorf("indexer client is required")
	}
	// 200 is well above the spec's 10-asset universe — one page is plenty.
	events, _, err := ix.ListEvents(ctx, indexerclient.ListEventsFilter{
		Kinds: []models.EventKind{models.EventKindAssetRegistered},
		Page:  indexerclient.Page{Number: 1, Size: 200},
	})
	if err != nil {
		return fmt.Errorf("seed aggregator registry: %w", err)
	}
	for _, e := range events {
		if e.AssetRegistered == nil {
			continue
		}
		// On-chain asset ids are keccak256(symbol); reverse-map them to the
		// catalog id via the precomputed table. Ids outside our 10-asset
		// universe are skipped.
		asset, ok := models.AssetByIDHash(e.AssetRegistered.AssetID)
		if !ok {
			continue
		}
		r.Set(asset.ID, e.AssetRegistered.Aggregator)
	}
	return nil
}

// Aggregator returns the cached aggregator address for the lowercase-symbol
// asset id, or false if the asset hasn't been observed yet.
func (r *Registry) Aggregator(assetID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addr, ok := r.byAsset[models.NormaliseAssetID(assetID)]
	return addr, ok
}

// Set inserts or updates the aggregator address for the asset id. The asset
// id is normalised before storage so lookups are case-insensitive.
func (r *Registry) Set(assetID, address string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAsset[models.NormaliseAssetID(assetID)] = address
}

// Snapshot returns a copy of the asset_id -> aggregator map. Callers can
// freely mutate the returned map; the registry's own state is untouched.
func (r *Registry) Snapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.byAsset))
	for k, v := range r.byAsset {
		out[k] = v
	}
	return out
}
