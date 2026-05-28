package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/config"
	"github.com/asolovov/evm-oracle-demo-api/internal/aggregatorregistry"
	"github.com/asolovov/evm-oracle-demo-api/internal/indexerclient"
	"github.com/asolovov/evm-oracle-demo-api/internal/models"
	"github.com/asolovov/evm-oracle-demo-api/internal/priceclient"
)

// --- mocks ---------------------------------------------------------------

type priceMock struct {
	prices  map[string]models.AggregatedPrice
	notFound map[string]bool
	err     error
}

func (m *priceMock) GetPrice(_ context.Context, assetID string) (models.AggregatedPrice, error) {
	if m.err != nil {
		return models.AggregatedPrice{}, m.err
	}
	if m.notFound[assetID] {
		return models.AggregatedPrice{}, priceclient.ErrNotFound
	}
	p, ok := m.prices[assetID]
	if !ok {
		return models.AggregatedPrice{}, priceclient.ErrNotFound
	}
	return p, nil
}

func (m *priceMock) Subscribe(_ context.Context, _ []string, _ func(models.AggregatedPrice)) error {
	return nil
}

func (m *priceMock) Close() error { return nil }

type indexerMock struct {
	requests map[string]models.RequestSummary
	events   []models.Event
}

func (m *indexerMock) ListEvents(_ context.Context, _ indexerclient.ListEventsFilter) ([]models.Event, indexerclient.PageInfo, error) {
	return m.events, indexerclient.PageInfo{}, nil
}

func (m *indexerMock) GetRequest(_ context.Context, reqID string) (models.RequestSummary, error) {
	s, ok := m.requests[reqID]
	if !ok {
		return models.RequestSummary{}, indexerclient.ErrNotFound
	}
	return s, nil
}

func (m *indexerMock) StreamEvents(_ context.Context, _ indexerclient.StreamEventsFilter, _ func(models.Event)) error {
	return nil
}

func (m *indexerMock) Close() error { return nil }

// --- setup ---------------------------------------------------------------

func newTestRouter(t *testing.T, api *API) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	api.Register(r, func(next http.Handler) http.Handler { return next })
	return r
}

func newTestAPI() (*API, *priceMock, *indexerMock) {
	price := &priceMock{prices: map[string]models.AggregatedPrice{}, notFound: map[string]bool{}}
	indexer := &indexerMock{requests: map[string]models.RequestSummary{}}
	registry := aggregatorregistry.New()
	registry.Set("weth", "0x000000000000000000000000000000000000bEEF")

	return &API{
		Price:    price,
		Indexer:  indexer,
		Registry: registry,
		Author: config.AuthorConfig{
			Name:  "Andrei Solovov",
			Links: map[string]string{"github": "https://github.com/asolovov"},
		},
		Chain: config.ChainConfig{
			ChainID:         11155111,
			Name:            "ethereum-sepolia",
			RegistryAddress: "0x89a6c12a403733c6a817472cec46a530581cb7ef",
		},
		Version:   "test",
		ServiceID: "evm-oracle-demo-api",
	}, price, indexer
}

// --- tests ---------------------------------------------------------------

func TestHealth(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", rec.Code)
	}
	var body models.HealthResponse
	mustJSON(t, rec, &body)
	if body.Status != "ok" || body.Author.Name == "" {
		t.Fatalf("health body unexpected: %+v", body)
	}
	if body.Author.Links["github"] == "" {
		t.Fatalf("expected github link in author block: %+v", body.Author)
	}
	if got := rec.Header().Get("X-Request-Id"); got == "" {
		t.Fatalf("expected X-Request-Id header to be set")
	}
}

func TestListAssetsReturnsCatalog(t *testing.T) {
	api, price, _ := newTestAPI()
	price.prices["weth"] = models.AggregatedPrice{
		AssetID: "weth", MedianPrice: 3450.20, AggregatedAt: time.Now().UTC(),
	}
	// every other asset returns ErrNotFound from the mock — the handler
	// should still include them with no LatestPrice block.

	r := newTestRouter(t, api)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Assets []models.AssetSummary `json:"assets"`
	}
	mustJSON(t, rec, &body)
	if len(body.Assets) != 10 {
		t.Fatalf("expected 10 assets in response, got %d", len(body.Assets))
	}
	var weth *models.AssetSummary
	for i := range body.Assets {
		if body.Assets[i].ID == "weth" {
			weth = &body.Assets[i]
		}
	}
	if weth == nil || weth.LatestPrice == nil || weth.LatestPrice.MedianPrice != 3450.20 {
		t.Fatalf("WETH summary missing latest price: %+v", weth)
	}
	if weth.AggregatorAddress == "" {
		t.Fatalf("expected WETH aggregator address to be populated from the registry")
	}
}

func TestGetAssetPriceNotFound(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/doge/price", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset should 404, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/weth/price", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing price should 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAssetPriceOK(t *testing.T) {
	api, price, _ := newTestAPI()
	price.prices["weth"] = models.AggregatedPrice{
		AssetID: "weth", MedianPrice: 3450.20, AggregatedAt: time.Now().UTC(),
	}
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/WETH/price", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body models.PriceDetail
	mustJSON(t, rec, &body)
	if body.Asset.ID != "weth" || body.AggregatedPrice.MedianPrice != 3450.20 {
		t.Fatalf("body unexpected: %+v", body)
	}
}

func TestGetAssetHistoryReturns501(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/weth/history", nil))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("history should be 501, got %d", rec.Code)
	}
}

func TestGetRequestNotFound(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests/42", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing request should 404, got %d", rec.Code)
	}
}

func TestGetRequestRejectsNonDecimal(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests/0xabc", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hex req_id should 400, got %d", rec.Code)
	}
}

func TestGetRequestOK(t *testing.T) {
	api, _, indexer := newTestAPI()
	indexer.requests["42"] = models.RequestSummary{
		ReqID: "42", AssetID: "weth", Status: models.RequestStatusPending,
	}
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestBuildTxValidatesInputs(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	// Bad JSON.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests/build-tx", strings.NewReader("not-json")))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON should 400, got %d", rec.Code)
	}

	// Unknown asset.
	body, _ := json.Marshal(BuildTxRequest{AssetID: "doge"})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests/build-tx", bytes.NewReader(body)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset should 404, got %d", rec.Code)
	}

	// Chain mismatch.
	body, _ = json.Marshal(BuildTxRequest{AssetID: "weth", ChainID: 1})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests/build-tx", bytes.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("chain mismatch should 400, got %d", rec.Code)
	}
}

func TestBuildTxOK(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	body, _ := json.Marshal(BuildTxRequest{AssetID: "weth", ChainID: 11155111})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests/build-tx", bytes.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp BuildTxResponse
	mustJSON(t, rec, &resp)
	if !strings.HasPrefix(resp.Data, "0x") || len(resp.Data) != 2+8+64 {
		t.Fatalf("expected 0x + 4-byte selector + 32-byte arg (74 chars total), got %q (len %d)", resp.Data, len(resp.Data))
	}
	if resp.ChainID != 11155111 || resp.Value != "0" {
		t.Fatalf("response unexpected: %+v", resp)
	}
	if resp.To == "" || resp.To == "0x" {
		t.Fatalf("expected aggregator address in 'to', got %q", resp.To)
	}
}

func TestListAssetsAttachesLastOnChain(t *testing.T) {
	api, _, indexer := newTestAPI()
	now := time.Now().UTC()
	indexer.events = []models.Event{{
		Meta: models.EventMeta{TxHash: "0xfeed", ObservedAt: now},
		Kind: models.EventKindPriceFulfilled,
		PriceFulfilled: &models.PriceFulfilledEvent{
			ReqID: "42", AssetID: aggregatorregistry.AssetIDToBytes32Hex("weth"),
			Price: "345020000000", RoundID: "7",
		},
	}}
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Assets []models.AssetSummary `json:"assets"`
	}
	mustJSON(t, rec, &body)
	var weth *models.AssetSummary
	for i := range body.Assets {
		if body.Assets[i].ID == "weth" {
			weth = &body.Assets[i]
		}
	}
	if weth == nil || weth.LastOnChainPrice != "345020000000" || weth.LastOnChainTx != "0xfeed" {
		t.Fatalf("LastOnChain* not attached: %+v", weth)
	}
}

func TestGetAssetPriceAttachesLastOnChain(t *testing.T) {
	api, price, indexer := newTestAPI()
	price.prices["weth"] = models.AggregatedPrice{AssetID: "weth", MedianPrice: 3450.20}
	now := time.Now().UTC()
	indexer.events = []models.Event{{
		Meta: models.EventMeta{TxHash: "0xfeed", ObservedAt: now},
		Kind: models.EventKindPriceFulfilled,
		PriceFulfilled: &models.PriceFulfilledEvent{
			ReqID: "42", AssetID: aggregatorregistry.AssetIDToBytes32Hex("weth"),
			Price: "345020000000", RoundID: "7",
		},
	}}
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/weth/price", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var detail models.PriceDetail
	mustJSON(t, rec, &detail)
	if detail.LastOnChainPrice != "345020000000" || detail.LastRoundID != "7" || detail.LastOnChainTx != "0xfeed" {
		t.Fatalf("LastOnChain* not attached: %+v", detail)
	}
}

func TestGetAssetPriceUpstreamErrorReturns502(t *testing.T) {
	api, price, _ := newTestAPI()
	price.err = fakeError("price-service died")
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/weth/price", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("upstream error should 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAssetHistoryUnknownAsset(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/assets/doge/history", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown asset history should 404, got %d", rec.Code)
	}
}

func TestGetRequestEmptyReqID(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	// chi normalises a trailing-slash path; the empty-id case is exercised
	// here by sending whitespace which the handler trims to "".
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/requests/%20%20", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("whitespace req_id should 400, got %d", rec.Code)
	}
}

type fakeError string

func (f fakeError) Error() string { return string(f) }

func TestBuildTxUnresolvedAggregator(t *testing.T) {
	api, _, _ := newTestAPI()
	// Drop the WETH entry to simulate the registry not yet seeded.
	api.Registry = aggregatorregistry.New()
	r := newTestRouter(t, api)

	body, _ := json.Marshal(BuildTxRequest{AssetID: "weth"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/requests/build-tx", bytes.NewReader(body)))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing aggregator should 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDocsServesSwaggerUI(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/docs status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("/docs Content-Type = %q, want text/html*", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "id=\"swagger-ui\"") {
		t.Fatalf("/docs body missing swagger-ui mount point")
	}
	if !strings.Contains(body, "/api/v1/openapi.yaml") {
		t.Fatalf("/docs body does not reference the spec URL")
	}
}

func TestOpenAPISpecServesEmbeddedYAML(t *testing.T) {
	api, _, _ := newTestAPI()
	r := newTestRouter(t, api)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.yaml", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/openapi.yaml status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/yaml") {
		t.Fatalf("/openapi.yaml Content-Type = %q, want application/yaml*", got)
	}
	body := rec.Body.String()
	for _, marker := range []string{
		"openapi: 3.1.0",
		"/api/v1/health:",
		"/api/v1/requests/build-tx:",
		"BuildTxResponse:",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("/openapi.yaml body missing %q", marker)
		}
	}
}

// mustJSON decodes the recorder body into out, failing the test on error.
func mustJSON(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
}
