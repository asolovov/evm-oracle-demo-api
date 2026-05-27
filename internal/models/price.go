package models

import (
	"time"

	pricev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/price/v1"
)

// SourceContribution mirrors price.v1.SourceContribution but is shaped for the
// dashboard's source-breakdown table.
type SourceContribution struct {
	Source           string    `json:"source"`
	Price            float64   `json:"price"`
	FetchedAt        time.Time `json:"fetched_at"`
	SourceObservedAt time.Time `json:"source_observed_at,omitempty"`
	AgeSec           int64     `json:"age_sec"`
	Included         bool      `json:"included"`
}

// AggregatedPrice is the per-asset median + per-source breakdown returned
// from price-service. The wire type uses doubles end-to-end; we never
// quantise to int256 in the BFF (that's oracle-service's concern).
type AggregatedPrice struct {
	AssetID      string               `json:"asset_id"`
	MedianPrice  float64              `json:"median_price"`
	AggregatedAt time.Time            `json:"aggregated_at"`
	Sources      []SourceContribution `json:"sources"`
}

// AggregatedPriceFromProto converts a price.v1.AggregatedPrice into the
// domain shape used by handlers and the WS hub.
func AggregatedPriceFromProto(p *pricev1.AggregatedPrice) AggregatedPrice {
	if p == nil {
		return AggregatedPrice{}
	}
	out := AggregatedPrice{
		AssetID:      p.GetAssetId(),
		MedianPrice:  p.GetMedianPrice(),
		AggregatedAt: p.GetAggregatedAt().AsTime(),
		Sources:      make([]SourceContribution, 0, len(p.GetSources())),
	}
	for _, s := range p.GetSources() {
		out.Sources = append(out.Sources, SourceContribution{
			Source:           s.GetSource(),
			Price:            s.GetPrice(),
			FetchedAt:        s.GetFetchedAt().AsTime(),
			SourceObservedAt: s.GetSourceObservedAt().AsTime(),
			AgeSec:           s.GetAgeSec(),
			Included:         s.GetIncluded(),
		})
	}
	return out
}

// AssetSummary is the per-tile entry returned by GET /api/v1/assets. It
// merges static catalog metadata with the latest aggregated price + last
// observed on-chain fulfilment.
type AssetSummary struct {
	Asset
	LatestPrice      *AggregatedPrice `json:"latest_price,omitempty"`
	LastOnChainPrice string           `json:"last_on_chain_price,omitempty"`
	LastOnChainAt    *time.Time       `json:"last_on_chain_at,omitempty"`
	LastOnChainTx    string           `json:"last_on_chain_tx,omitempty"`
}

// PriceDetail is the response body for GET /api/v1/assets/{id}/price — the
// drill-down page. Carries both the off-chain (aggregator) view and the
// most recent on-chain round so reviewers can spot drift.
type PriceDetail struct {
	Asset            Asset                `json:"asset"`
	AggregatedPrice  AggregatedPrice      `json:"aggregated_price"`
	LastOnChainPrice string               `json:"last_on_chain_price,omitempty"`
	LastOnChainAt    *time.Time           `json:"last_on_chain_at,omitempty"`
	LastOnChainTx    string               `json:"last_on_chain_tx,omitempty"`
	LastRoundID      string               `json:"last_round_id,omitempty"`
	Sources          []SourceContribution `json:"sources"`
}

// HistoryPoint is one row of the price history time-series.
type HistoryPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Price     float64   `json:"price"`
}
