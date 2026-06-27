package models

import (
	"encoding/hex"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// On-chain asset ids are NOT ASCII-encoded symbols — the contracts derive
// them as `keccak256(symbol)` over the UTF-8 bytes of the UPPERCASE ticker,
// matching evm-oracle-demo-contracts' `config/assets.ts`
// (`assetId: keccak256(toBytes(symbol))`). Verified against the live
// deployment: keccak256("WETH") == 0x0f8a193ff464434486c0daf7db2a895884365d2bc84ba47a68fcf89c1b14b5b8,
// the registered WETH assetId.
//
// A keccak hash is one-way, so we cannot decode a bytes32 id back to a
// symbol. Instead we precompute the hash for every catalog asset once and
// keep both directions as lookup tables.
var (
	assetIDHashByID    = make(map[string]string, len(AssetCatalog)) // lowercase id -> 0x bytes32 hex
	assetByIDHashLower = make(map[string]Asset, len(AssetCatalog))  // lowercase 0x bytes32 hex -> Asset
)

//nolint:gochecknoinits // derive the keccak asset-id tables once from the static catalog at load.
func init() {
	for _, a := range AssetCatalog {
		// Uppercase the symbol defensively: the contract convention is
		// keccak256 over the UPPERCASE ticker, so a future catalog entry
		// with a non-uppercase Symbol still derives the on-chain id. (The
		// deployment-pinned test in assetid_test.go is the ultimate guard.)
		h := "0x" + hex.EncodeToString(crypto.Keccak256([]byte(strings.ToUpper(a.Symbol))))
		assetIDHashByID[a.ID] = h
		assetByIDHashLower[h] = a
	}
}

// AssetIDHash returns the on-chain bytes32 asset id (0x-prefixed lowercase
// hex) for a catalog asset. The lookup key is normalised to the lowercase
// canonical id, so both the id ("weth") and the symbol ("WETH") resolve as
// long as the symbol lowercases to the id (true for every catalog asset).
// Returns false if the asset is not tracked.
func AssetIDHash(idOrSymbol string) (string, bool) {
	h, ok := assetIDHashByID[NormaliseAssetID(idOrSymbol)]
	return h, ok
}

// AssetByIDHash reverse-looks-up the catalog asset for an on-chain bytes32
// asset id. Input is matched case-insensitively against the precomputed
// keccak hashes; returns false for any id outside the demo's 10-asset
// universe (e.g. an asset registered on chain that the BFF doesn't surface).
func AssetByIDHash(hash string) (Asset, bool) {
	a, ok := assetByIDHashLower[strings.ToLower(strings.TrimSpace(hash))]
	return a, ok
}
