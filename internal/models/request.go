package models

import (
	"time"

	indexerv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/indexer/v1"
)

// RequestStatusKind mirrors indexer.v1.RequestStatus.Status.
type RequestStatusKind string

const (
	RequestStatusUnspecified RequestStatusKind = ""
	RequestStatusPending     RequestStatusKind = "pending"
	RequestStatusFulfilled   RequestStatusKind = "fulfilled"
	RequestStatusFailed      RequestStatusKind = "failed"
)

// String returns the wire-format value.
func (s RequestStatusKind) String() string { return string(s) }

// RequestStatusFromProto maps the protobuf enum onto the domain enum.
func RequestStatusFromProto(s indexerv1.RequestStatus_Status) RequestStatusKind {
	switch s {
	case indexerv1.RequestStatus_STATUS_PENDING:
		return RequestStatusPending
	case indexerv1.RequestStatus_STATUS_FULFILLED:
		return RequestStatusFulfilled
	case indexerv1.RequestStatus_STATUS_FAILED:
		return RequestStatusFailed
	default:
		return RequestStatusUnspecified
	}
}

// RequestSummary is the response body for GET /api/v1/requests/{reqId}.
type RequestSummary struct {
	ReqID           string            `json:"req_id"`
	AssetID         string            `json:"asset_id"`
	Status          RequestStatusKind `json:"status"`
	Requester       string            `json:"requester"`
	RequestedTxHash string            `json:"requested_tx_hash"`
	FulfilledTxHash string            `json:"fulfilled_tx_hash,omitempty"`
	FulfilledPrice  string            `json:"fulfilled_price,omitempty"`
	RequestedAt     *time.Time        `json:"requested_at,omitempty"`
	FulfilledAt     *time.Time        `json:"fulfilled_at,omitempty"`
}

// RequestSummaryFromProto converts indexer.v1.RequestStatus to the domain
// shape. Zero-valued timestamps surface as nil (omitted from the JSON
// response) so the frontend can distinguish "not fulfilled" from
// "fulfilled at epoch zero".
func RequestSummaryFromProto(r *indexerv1.RequestStatus) RequestSummary {
	if r == nil {
		return RequestSummary{}
	}
	out := RequestSummary{
		ReqID:           r.GetReqId(),
		AssetID:         r.GetAssetId(),
		Status:          RequestStatusFromProto(r.GetStatus()),
		Requester:       r.GetRequester(),
		RequestedTxHash: r.GetRequestedTxHash(),
		FulfilledTxHash: r.GetFulfilledTxHash(),
		FulfilledPrice:  r.GetFulfilledPrice(),
	}
	if ts := r.GetRequestedAt(); ts != nil && (ts.GetSeconds() != 0 || ts.GetNanos() != 0) {
		t := ts.AsTime()
		out.RequestedAt = &t
	}
	if ts := r.GetFulfilledAt(); ts != nil && (ts.GetSeconds() != 0 || ts.GetNanos() != 0) {
		t := ts.AsTime()
		out.FulfilledAt = &t
	}
	return out
}
