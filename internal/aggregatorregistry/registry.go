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
	"encoding/hex"
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
		assetID, ok := AssetIDFromBytes32Hex(e.AssetRegistered.AssetID)
		if !ok {
			continue
		}
		r.Set(assetID, e.AssetRegistered.Aggregator)
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

// AssetIDToBytes32Hex converts a lowercase-symbol asset id ("weth") to the
// on-chain bytes32 form used by the contract layer. Right-pads with zeros
// to 32 bytes; result is `0x` + 64 lowercase hex chars.
func AssetIDToBytes32Hex(id string) string {
	id = models.NormaliseAssetID(id)
	var b [32]byte
	copy(b[:], id)
	return "0x" + hex.EncodeToString(b[:])
}

// AssetIDFromBytes32Hex inverts AssetIDToBytes32Hex: take a 0x-prefixed
// 32-byte hex string, strip trailing zero padding, decode the ASCII symbol.
// Returns false if input is malformed.
func AssetIDFromBytes32Hex(s string) (string, bool) {
	if len(s) < 2 || s[:2] != "0x" && s[:2] != "0X" {
		return "", false
	}
	raw, err := hex.DecodeString(s[2:])
	if err != nil || len(raw) != 32 {
		return "", false
	}
	// Strip trailing 0x00 padding.
	end := 32
	for end > 0 && raw[end-1] == 0 {
		end--
	}
	if end == 0 {
		return "", false
	}
	out := string(raw[:end])
	// Validate ASCII lower / digit only; reject any non-printable.
	for i := 0; i < len(out); i++ {
		c := out[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return "", false
		}
	}
	return out, true
}
