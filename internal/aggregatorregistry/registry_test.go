package aggregatorregistry

import (
	"context"
	"testing"

	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// wethIDHash is the deployed keccak256("WETH") asset id.
const wethIDHash = "0x0f8a193ff464434486c0daf7db2a895884365d2bc84ba47a68fcf89c1b14b5b8"

type stubIndexer struct {
	events []models.Event
}

func (s *stubIndexer) ListEvents(_ context.Context, _ indexerclient.ListEventsFilter) ([]models.Event, indexerclient.PageInfo, error) {
	return s.events, indexerclient.PageInfo{}, nil
}
func (s *stubIndexer) GetRequest(_ context.Context, _ string) (models.RequestSummary, error) {
	return models.RequestSummary{}, indexerclient.ErrNotFound
}
func (s *stubIndexer) StreamEvents(_ context.Context, _ indexerclient.StreamEventsFilter, _ func(models.Event)) error {
	return nil
}
func (s *stubIndexer) Close() error { return nil }

func TestLoadReverseMapsKeccakAssetIDs(t *testing.T) {
	ix := &stubIndexer{events: []models.Event{
		{
			Kind: models.EventKindAssetRegistered,
			AssetRegistered: &models.AssetRegisteredEvent{
				AssetID:    wethIDHash,
				Aggregator: "0x075be31662c2548c4e940d7e769c328a34dcb281",
			},
		},
		{
			// An asset id outside the 10-asset catalog — must be skipped, not stored.
			Kind: models.EventKindAssetRegistered,
			AssetRegistered: &models.AssetRegisteredEvent{
				AssetID:    "0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef0",
				Aggregator: "0xstranger",
			},
		},
	}}

	r := New()
	if err := r.Load(context.Background(), ix); err != nil {
		t.Fatalf("Load: %v", err)
	}
	addr, ok := r.Aggregator("weth")
	if !ok || addr != "0x075be31662c2548c4e940d7e769c328a34dcb281" {
		t.Fatalf("WETH aggregator not seeded from keccak asset id: %q ok=%v", addr, ok)
	}
	if len(r.Snapshot()) != 1 {
		t.Fatalf("expected only the catalog asset to be stored, got %v", r.Snapshot())
	}
}

func TestAggregatorCaseInsensitive(t *testing.T) {
	r := New()
	r.Set("WETH", "0xagg")
	if addr, ok := r.Aggregator("weth"); !ok || addr != "0xagg" {
		t.Fatalf("Aggregator(weth) = %q ok=%v", addr, ok)
	}
	if addr, ok := r.Aggregator("Weth"); !ok || addr != "0xagg" {
		t.Fatalf("Aggregator(Weth) = %q ok=%v", addr, ok)
	}
	if _, ok := r.Aggregator("doge"); ok {
		t.Fatalf("Aggregator(doge) should not match")
	}
}

func TestSnapshotIsIndependent(t *testing.T) {
	r := New()
	r.Set("weth", "0xagg1")
	snap := r.Snapshot()
	snap["weth"] = "mutated"
	if addr, _ := r.Aggregator("weth"); addr != "0xagg1" {
		t.Fatalf("registry mutated through snapshot: got %q", addr)
	}
}
