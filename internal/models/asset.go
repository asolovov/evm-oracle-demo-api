// Package models holds the BFF's domain types and the proto ↔ domain
// conversion methods. Per architecture rule 3, every parsing / conversion
// method on a domain model lives here, not in handlers or clients.
package models

import "fmt"

// AssetClass discriminates the two asset universes the demo covers.
type AssetClass string

const (
	// AssetClassCrypto — exchange-native assets priced via DEX + CEX feeds.
	AssetClassCrypto AssetClass = "crypto"

	// AssetClassRWA — real-world assets priced via external data vendors.
	AssetClassRWA AssetClass = "rwa"
)

// String returns the wire-format value.
func (c AssetClass) String() string { return string(c) }

// Asset is the catalog entry for one tracked instrument. Aggregator
// addresses are resolved at runtime from the indexer's AssetRegistered
// events; they live alongside the asset in the catalog so REST responses
// can quote them.
type Asset struct {
	ID                string     `json:"id"`
	Symbol            string     `json:"symbol"`
	Name              string     `json:"name"`
	Class             AssetClass `json:"class"`
	AggregatorAddress string     `json:"aggregator_address,omitempty"`
}

// AssetCatalog is the static list of assets the demo covers. The ID column
// uses the lowercase-symbol form the upstream protos document (see
// price.proto / indexer.proto: "Canonical asset id: lowercase symbol used
// as the bytes32 key on chain").
//
// This list is the canonical surface — the BFF refuses to serve any other
// asset id even if the upstream services have additional data.
var AssetCatalog = []Asset{
	{ID: "weth", Symbol: "WETH", Name: "Wrapped Ether", Class: AssetClassCrypto},
	{ID: "wbtc", Symbol: "WBTC", Name: "Wrapped Bitcoin", Class: AssetClassCrypto},
	{ID: "link", Symbol: "LINK", Name: "Chainlink", Class: AssetClassCrypto},
	{ID: "uni", Symbol: "UNI", Name: "Uniswap", Class: AssetClassCrypto},
	{ID: "aave", Symbol: "AAVE", Name: "Aave", Class: AssetClassCrypto},
	{ID: "xau", Symbol: "XAU", Name: "Gold (troy ounce)", Class: AssetClassRWA},
	{ID: "xag", Symbol: "XAG", Name: "Silver (troy ounce)", Class: AssetClassRWA},
	{ID: "spx", Symbol: "SPX", Name: "S&P 500 Index", Class: AssetClassRWA},
	{ID: "wti", Symbol: "WTI", Name: "Crude Oil (WTI)", Class: AssetClassRWA},
	{ID: "hg", Symbol: "HG", Name: "Copper (HG)", Class: AssetClassRWA},
}

// FindAsset returns the catalog entry for the given id, or false if the
// id is not tracked. Comparison is case-insensitive on the id.
func FindAsset(id string) (Asset, bool) {
	id = NormaliseAssetID(id)
	for _, a := range AssetCatalog {
		if a.ID == id {
			return a, true
		}
	}
	return Asset{}, false
}

// NormaliseAssetID returns the canonical lowercase id for matching against
// the catalog. ASCII-only lower (the catalog ids are ASCII).
func NormaliseAssetID(id string) string {
	b := make([]byte, len(id))
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// AssetIDs returns the catalog ids in stable catalog order — the order WS
// subscriptions and the asset-summary tile rendering rely on.
func AssetIDs() []string {
	out := make([]string, len(AssetCatalog))
	for i, a := range AssetCatalog {
		out[i] = a.ID
	}
	return out
}

// ValidateAssetID returns an error if the id is not tracked. Used by
// handlers as the first validation step on user-supplied path params.
func ValidateAssetID(id string) error {
	if _, ok := FindAsset(id); !ok {
		return fmt.Errorf("asset %q is not tracked", id)
	}
	return nil
}
