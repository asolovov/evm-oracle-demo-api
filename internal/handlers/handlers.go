// Package handlers implements the /api/v1/* HTTP surface. Handlers wrap the
// gRPC client interfaces and the aggregator registry; nothing here knows about
// the chi router or the http.Server lifecycle.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/aggregatorregistry"
	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
	"github.com/asolovov/evm-oracle-demo-api/internal/priceclient"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

// API holds the dependencies every handler shares. Constructed once in
// application.go and registered on the chi router.
type API struct {
	Price     priceclient.Client
	Indexer   indexerclient.Client
	Registry  *aggregatorregistry.Registry
	Author    config.AuthorConfig
	Chain     config.ChainConfig
	Version   string
	ServiceID string
}

// Health serves GET /api/v1/health.
func (a *API) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, models.HealthResponse{
		Status:  "ok",
		Service: a.ServiceID,
		Version: a.Version,
		Author: models.AuthorResponse{
			Name:  a.Author.Name,
			Links: a.Author.Links,
		},
	})
}

// ListAssets serves GET /api/v1/assets. Joins the static catalog with the
// latest aggregated price + last on-chain fulfilment per asset. Upstream
// failures degrade gracefully — a missing price simply leaves the field out
// of the response so the dashboard can still render the tile.
func (a *API) ListAssets(w http.ResponseWriter, r *http.Request) {
	out := make([]models.AssetSummary, 0, len(models.AssetCatalog))
	for _, asset := range models.AssetCatalog {
		summary := models.AssetSummary{Asset: asset}
		if addr, ok := a.Registry.Aggregator(asset.ID); ok {
			summary.AggregatorAddress = addr
		}
		if price, err := a.Price.GetPrice(r.Context(), asset.ID); err == nil {
			p := price
			summary.LatestPrice = &p
		} else if !errors.Is(err, priceclient.ErrNotFound) {
			logger.Log().WithError(err).WithField("asset_id", asset.ID).Warn("list_assets: price.GetPrice failed")
		}
		summary = a.attachLastOnChain(r.Context(), asset, summary)
		out = append(out, summary)
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": out})
}

// GetAssetPrice serves GET /api/v1/assets/{id}/price.
func (a *API) GetAssetPrice(w http.ResponseWriter, r *http.Request) {
	assetID := models.NormaliseAssetID(chi.URLParam(r, "id"))
	asset, ok := models.FindAsset(assetID)
	if !ok {
		writeError(w, http.StatusNotFound, "asset_not_tracked", "asset is not tracked")
		return
	}

	price, err := a.Price.GetPrice(r.Context(), assetID)
	if err != nil {
		if errors.Is(err, priceclient.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no_price", "no aggregated price for this asset yet")
			return
		}
		logger.Log().WithError(err).Error("get_asset_price: price.GetPrice failed")
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "price-service unreachable")
		return
	}

	detail := models.PriceDetail{
		Asset:           asset,
		AggregatedPrice: price,
		Sources:         price.Sources,
	}
	if addr, ok := a.Registry.Aggregator(assetID); ok {
		detail.Asset.AggregatorAddress = addr
	}
	if last := a.latestOnChainRound(r.Context(), assetID); last != nil {
		detail.LastOnChainPrice = last.PriceFulfilled.Price
		detail.LastRoundID = last.PriceFulfilled.RoundID
		detail.LastOnChainTx = last.Meta.TxHash
		t := last.Meta.ObservedAt
		detail.LastOnChainAt = &t
	}

	writeJSON(w, http.StatusOK, detail)
}

// GetAssetHistory serves GET /api/v1/assets/{id}/history. v1 has no
// price-service history RPC; the endpoint surfaces a 501 with a clear pointer
// so the frontend can hide the chart until the RPC lands. Documented in the
// README under "known gaps".
func (a *API) GetAssetHistory(w http.ResponseWriter, r *http.Request) {
	assetID := models.NormaliseAssetID(chi.URLParam(r, "id"))
	if _, ok := models.FindAsset(assetID); !ok {
		writeError(w, http.StatusNotFound, "asset_not_tracked", "asset is not tracked")
		return
	}
	writeError(w, http.StatusNotImplemented, "history_not_available",
		"price history is not exposed by price-service v1; see README.md > Known gaps")
}

// GetRequest serves GET /api/v1/requests/{reqId}.
func (a *API) GetRequest(w http.ResponseWriter, r *http.Request) {
	reqID := strings.TrimSpace(chi.URLParam(r, "reqId"))
	if reqID == "" {
		writeError(w, http.StatusBadRequest, "invalid_req_id", "req_id is required")
		return
	}
	if !isDecimal(reqID) {
		writeError(w, http.StatusBadRequest, "invalid_req_id", "req_id must be a base-10 uint256")
		return
	}

	summary, err := a.Indexer.GetRequest(r.Context(), reqID)
	if err != nil {
		if errors.Is(err, indexerclient.ErrNotFound) {
			writeError(w, http.StatusNotFound, "request_not_found", "request not observed by indexer")
			return
		}
		logger.Log().WithError(err).Error("get_request: indexer.GetRequest failed")
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "indexer-service unreachable")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// BuildTxRequest is the body for POST /api/v1/requests/build-tx.
type BuildTxRequest struct {
	AssetID string `json:"asset_id"`
	// ChainID is optional. When set it MUST match the BFF's configured
	// chain — the BFF refuses to build calldata for a chain it doesn't
	// know about.
	ChainID int64 `json:"chain_id,omitempty"`
}

// BuildTxResponse is the body returned by POST /api/v1/requests/build-tx.
type BuildTxResponse struct {
	To        string `json:"to"`
	Data      string `json:"data"`
	Value     string `json:"value"`
	ChainID   int64  `json:"chain_id"`
	ChainName string `json:"chain_name,omitempty"`
}

// BuildTx serves POST /api/v1/requests/build-tx. Pure calldata builder —
// never submits. Returns the aggregator address, ABI-encoded requestPrice
// calldata, and a suggested native-token fee (0 in v1 — see README).
func (a *API) BuildTx(w http.ResponseWriter, r *http.Request) {
	var body BuildTxRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body is not valid JSON")
		return
	}

	assetID := models.NormaliseAssetID(body.AssetID)
	if _, ok := models.FindAsset(assetID); !ok {
		writeError(w, http.StatusNotFound, "asset_not_tracked", "asset is not tracked")
		return
	}
	if body.ChainID != 0 && body.ChainID != a.Chain.ChainID {
		writeError(w, http.StatusBadRequest, "chain_mismatch",
			"chain_id does not match this BFF's configured chain")
		return
	}

	aggregator, ok := a.Registry.Aggregator(assetID)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "aggregator_not_resolved",
			"aggregator address for this asset has not been observed yet; retry shortly")
		return
	}
	if !common.IsHexAddress(aggregator) {
		writeError(w, http.StatusInternalServerError, "registry_corrupt",
			"cached aggregator address is not a valid 20-byte hex address")
		return
	}

	calldata, err := encodeRequestPriceCalldata(assetID)
	if err != nil {
		logger.Log().WithError(err).Error("build_tx: calldata encoding failed")
		writeError(w, http.StatusInternalServerError, "calldata_encode_failed",
			"failed to encode requestPrice calldata")
		return
	}

	writeJSON(w, http.StatusOK, BuildTxResponse{
		To:        common.HexToAddress(aggregator).Hex(),
		Data:      calldata,
		Value:     "0",
		ChainID:   a.Chain.ChainID,
		ChainName: a.Chain.Name,
	})
}

// --- helpers -----------------------------------------------------------

// attachLastOnChain best-effort fills LastOnChain* from a single-asset
// indexer ListEvents query. Failures are logged but never propagated.
func (a *API) attachLastOnChain(ctx context.Context, asset models.Asset, summary models.AssetSummary) models.AssetSummary {
	if last := a.latestOnChainRound(ctx, asset.ID); last != nil {
		summary.LastOnChainPrice = last.PriceFulfilled.Price
		summary.LastOnChainTx = last.Meta.TxHash
		t := last.Meta.ObservedAt
		summary.LastOnChainAt = &t
	}
	return summary
}

func (a *API) latestOnChainRound(ctx context.Context, assetID string) *models.Event {
	events, _, err := a.Indexer.ListEvents(ctx, indexerclient.ListEventsFilter{
		Kinds:   []models.EventKind{models.EventKindPriceFulfilled},
		AssetID: aggregatorregistry.AssetIDToBytes32Hex(assetID),
		Page:    indexerclient.Page{Number: 1, Size: 1},
	})
	if err != nil {
		// Log at debug — a missing indexer is a documented graceful-degradation case.
		logger.Log().WithError(err).Debug("latest_on_chain: indexer.ListEvents failed")
		return nil
	}
	if len(events) == 0 || events[0].PriceFulfilled == nil {
		return nil
	}
	return &events[0]
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Log().WithError(err).Warn("writeJSON: encode failed")
	}
}

// ErrorResponse is the JSON shape returned for every non-2xx response.
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Code: code, Message: message})
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
