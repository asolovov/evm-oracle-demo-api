package models

import "testing"

// Known-good values from the live evm-oracle-demo-contracts deployment
// (deployments/ethereum-sepolia/addresses.json). If the contracts ever
// change their asset-id derivation, these assertions break loudly.
var deployedAssetIDs = map[string]string{
	"weth": "0x0f8a193ff464434486c0daf7db2a895884365d2bc84ba47a68fcf89c1b14b5b8",
	"wbtc": "0x98da2c5e4c6b1db946694570273b859a6e4083ccc8faa155edfc4c54eb3cfd73",
	"link": "0x921a3539bcb764c889432630877414523e7fbca00c211bc787aeae69e2e3a779",
	"uni":  "0xfba01d52a7cd84480d0573725899486a0b5e55c20ff45d6628874349375d1650",
	"aave": "0xde46fbfa339d54cd65b79d8320a7a53c78177565c2aaf4c8b13eed7865e7cfc8",
	"xau":  "0x7c687a3207cd9c05b4b11d8dd7ac337919c2200102d72989a597ebc5afcf180b",
	"xag":  "0x5ccc5c04130d272bf07d6e066f4cae40cfc0313643d815db3e17af00e6ebf601",
	"spx":  "0x1308465f1da3a6702b88abc29db16011bdb6f6a7cb404fee1daa81f8da9d9972",
	"wti":  "0x1f29567db4e0c1628fa0f8675c031b615246dd0dd3de399fdf8b5aec1829181d",
}

func TestAssetIDHashMatchesDeployment(t *testing.T) {
	for id, want := range deployedAssetIDs {
		got, ok := AssetIDHash(id)
		if !ok {
			t.Fatalf("AssetIDHash(%q) not found", id)
		}
		if got != want {
			t.Fatalf("AssetIDHash(%q) = %s, want %s (deployment)", id, got, want)
		}
	}
}

func TestAssetIDHashCaseInsensitive(t *testing.T) {
	lower, _ := AssetIDHash("weth")
	upper, _ := AssetIDHash("WETH")
	if lower == "" || lower != upper {
		t.Fatalf("AssetIDHash should be case-insensitive: %q vs %q", lower, upper)
	}
	if _, ok := AssetIDHash("doge"); ok {
		t.Fatalf("AssetIDHash(doge) should not resolve")
	}
}

func TestAssetByIDHashReverse(t *testing.T) {
	for id, hash := range deployedAssetIDs {
		a, ok := AssetByIDHash(hash)
		if !ok {
			t.Fatalf("AssetByIDHash(%s) not found (id %s)", hash, id)
		}
		if a.ID != id {
			t.Fatalf("AssetByIDHash(%s).ID = %q, want %q", hash, a.ID, id)
		}
	}

	// Mixed-case input still resolves.
	if _, ok := AssetByIDHash("0x0F8A193FF464434486C0DAF7DB2A895884365D2BC84BA47A68FCF89C1B14B5B8"); !ok {
		t.Fatalf("AssetByIDHash should be case-insensitive on the hex")
	}

	// An unknown / non-catalog hash resolves to false (not a panic).
	if _, ok := AssetByIDHash("0xdeadbeef"); ok {
		t.Fatalf("AssetByIDHash(garbage) should be false")
	}
}

func TestEveryCatalogAssetHasHash(t *testing.T) {
	for _, a := range AssetCatalog {
		h, ok := AssetIDHash(a.ID)
		if !ok || h == "" {
			t.Fatalf("catalog asset %q has no derived hash", a.ID)
		}
		back, ok := AssetByIDHash(h)
		if !ok || back.ID != a.ID {
			t.Fatalf("round-trip failed for %q (hash %s)", a.ID, h)
		}
	}
}
