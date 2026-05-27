package models

import "testing"

func TestAssetCatalogHasTenAssets(t *testing.T) {
	if len(AssetCatalog) != 10 {
		t.Fatalf("expected 10 catalog entries (5 crypto + 5 RWA), got %d", len(AssetCatalog))
	}

	var crypto, rwa int
	seen := make(map[string]bool)
	for _, a := range AssetCatalog {
		if seen[a.ID] {
			t.Fatalf("duplicate asset id %q in catalog", a.ID)
		}
		seen[a.ID] = true

		if a.ID != NormaliseAssetID(a.ID) {
			t.Fatalf("catalog entry %q is not in normalised (lowercase) form", a.ID)
		}

		switch a.Class {
		case AssetClassCrypto:
			crypto++
		case AssetClassRWA:
			rwa++
		default:
			t.Fatalf("asset %q has unknown class %q", a.ID, a.Class)
		}
	}
	if crypto != 5 || rwa != 5 {
		t.Fatalf("expected 5 crypto + 5 RWA, got crypto=%d rwa=%d", crypto, rwa)
	}
}

func TestFindAssetCaseInsensitive(t *testing.T) {
	for _, id := range []string{"WETH", "Weth", "weth"} {
		a, ok := FindAsset(id)
		if !ok {
			t.Fatalf("FindAsset(%q) returned false", id)
		}
		if a.ID != "weth" {
			t.Fatalf("FindAsset(%q).ID = %q, want %q", id, a.ID, "weth")
		}
	}

	if _, ok := FindAsset("doge"); ok {
		t.Fatalf("FindAsset(\"doge\") should not have matched")
	}
}

func TestValidateAssetID(t *testing.T) {
	if err := ValidateAssetID("weth"); err != nil {
		t.Fatalf("ValidateAssetID(\"weth\"): %v", err)
	}
	if err := ValidateAssetID("doge"); err == nil {
		t.Fatalf("ValidateAssetID(\"doge\") returned nil, want error")
	}
}

func TestAssetIDsPreservesOrder(t *testing.T) {
	ids := AssetIDs()
	if len(ids) != len(AssetCatalog) {
		t.Fatalf("AssetIDs len = %d, catalog len = %d", len(ids), len(AssetCatalog))
	}
	for i, a := range AssetCatalog {
		if ids[i] != a.ID {
			t.Fatalf("AssetIDs[%d] = %q, want %q", i, ids[i], a.ID)
		}
	}
}
