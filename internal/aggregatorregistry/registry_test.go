package aggregatorregistry

import (
	"testing"
)

func TestAssetIDRoundTrip(t *testing.T) {
	tests := []string{"weth", "wbtc", "xau", "hg", "spx"}
	for _, id := range tests {
		hex := AssetIDToBytes32Hex(id)
		got, ok := AssetIDFromBytes32Hex(hex)
		if !ok {
			t.Fatalf("round-trip failed for %q (hex %q)", id, hex)
		}
		if got != id {
			t.Fatalf("round-trip mismatch: got %q want %q (hex %q)", got, id, hex)
		}
	}
}

func TestAssetIDFromBytes32HexRejectsGarbage(t *testing.T) {
	for _, bad := range []string{
		"weth",            // no 0x
		"0x",              // empty
		"0xzz",            // not hex
		"0x" + hexFull(0), // all zero
	} {
		if got, ok := AssetIDFromBytes32Hex(bad); ok {
			t.Fatalf("expected reject for %q, got %q ok=true", bad, got)
		}
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

func hexFull(b byte) string {
	out := make([]byte, 64)
	hex := "0123456789abcdef"
	for i := 0; i < 64; i++ {
		_ = b
		out[i] = hex[0]
	}
	return string(out)
}
