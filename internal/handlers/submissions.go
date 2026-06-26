package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/asolovov/evm-oracle-demo-api/internal/models"
	"github.com/asolovov/evm-oracle-demo-api/internal/oracleclient"
	"github.com/asolovov/evm-oracle-demo-api/pkg/logger"
)

const (
	defaultSubmissionsPageSize int32 = 25
	maxSubmissionsPageSize     int32 = 100
	// maxPageNumber bounds the 1-indexed page so the int32 the upstream
	// proto expects can never overflow on absurd input.
	maxPageNumber int32 = 1 << 20
)

// ListSubmissions serves GET /api/v1/submissions. Paginated submission
// history from oracle-service, optionally filtered by `?asset_id`. Page
// controls: `?page` (1-indexed) + `?page_size` (capped at 100).
func (a *API) ListSubmissions(w http.ResponseWriter, r *http.Request) {
	assetID := models.NormaliseAssetID(r.URL.Query().Get("asset_id"))
	if assetID != "" {
		if _, ok := models.FindAsset(assetID); !ok {
			writeError(w, http.StatusBadRequest, "asset_not_tracked", "asset_id filter is not a tracked asset")
			return
		}
	}

	page := parsePositivePageParam(r.URL.Query().Get("page"), 1)
	pageSize := parsePositivePageParam(r.URL.Query().Get("page_size"), defaultSubmissionsPageSize)
	if pageSize > maxSubmissionsPageSize {
		pageSize = maxSubmissionsPageSize
	}

	subs, pageInfo, err := a.Oracle.ListSubmissions(r.Context(), oracleclient.ListSubmissionsFilter{
		AssetID: assetID,
		Page:    oracleclient.Page{Number: page, Size: pageSize},
	})
	if err != nil {
		logger.Log().WithError(err).Error("list_submissions: oracle.ListSubmissions failed")
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "oracle-service unreachable")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"submissions": subs,
		"page": map[string]any{
			"number":      pageInfo.Number,
			"size":        pageInfo.Size,
			"total_items": pageInfo.TotalItems,
			"total_pages": pageInfo.TotalPages,
		},
	})
}

// GetSubmission serves GET /api/v1/submissions/{id}. The id is dispatched by
// shape: a 0x-prefixed 32-byte hex string is treated as a tx hash (the only
// way to reach heartbeat submissions, which carry req_id "0"); an all-decimal
// non-zero string is treated as a consumer req_id. "0" and anything else are
// rejected — per the proto, req_id "0" cannot be looked up directly.
func (a *API) GetSubmission(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	var (
		sub models.SubmissionStatus
		err error
	)
	switch {
	case isTxHash(id):
		sub, err = a.Oracle.GetSubmissionByTxHash(r.Context(), strings.ToLower(id))
	case isDecimal(id) && id != "0":
		sub, err = a.Oracle.GetSubmissionByReqID(r.Context(), id)
	default:
		writeError(w, http.StatusBadRequest, "invalid_submission_id",
			"id must be a base-10 req_id (non-zero) or a 0x-prefixed 32-byte tx hash")
		return
	}

	if err != nil {
		switch {
		case errors.Is(err, oracleclient.ErrNotFound):
			writeError(w, http.StatusNotFound, "submission_not_found", "no submission for that id")
		case errors.Is(err, oracleclient.ErrInvalidArgument):
			writeError(w, http.StatusBadRequest, "invalid_submission_id", "oracle rejected the lookup key")
		default:
			logger.Log().WithError(err).Error("get_submission: oracle.GetSubmissionStatus failed")
			writeError(w, http.StatusBadGateway, "upstream_unavailable", "oracle-service unreachable")
		}
		return
	}

	writeJSON(w, http.StatusOK, sub)
}

// isTxHash reports whether s is a 0x-prefixed 32-byte (64 hex char) hash.
func isTxHash(s string) bool {
	if len(s) != 66 || (s[:2] != "0x" && s[:2] != "0X") {
		return false
	}
	for i := 2; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// parsePositivePageParam parses s as a positive page parameter, falling back
// to def on empty or invalid input and clamping to maxPageNumber so the
// int32 cast is provably overflow-free.
func parsePositivePageParam(s string, def int32) int32 {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return def
	}
	if n > int(maxPageNumber) {
		return maxPageNumber
	}
	//nolint:gosec // G109: n is clamped to [1, maxPageNumber] (2^20) above, so the int32 cast cannot overflow.
	return int32(n)
}
