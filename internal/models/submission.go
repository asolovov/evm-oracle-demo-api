package models

import (
	"time"

	oraclev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/oracle/v1"
)

// SubmissionStatusKind mirrors oracle.v1.SubmissionStatus.Status — the
// lifecycle of one on-chain `fulfillPrice` transaction.
type SubmissionStatusKind string

// SubmissionStatus* — the oracle's terminal + in-flight states for a
// submission. `expired` is the newest (added 2026-05): a request abandoned
// before broadcast because its TTL elapsed while still queued/processing/
// signing. Distinct from `failed` — nothing was sent on-chain and no nonce
// was consumed.
const (
	SubmissionStatusUnspecified SubmissionStatusKind = ""
	SubmissionStatusPending     SubmissionStatusKind = "pending"
	SubmissionStatusConfirmed   SubmissionStatusKind = "confirmed"
	SubmissionStatusFailed      SubmissionStatusKind = "failed"
	SubmissionStatusDropped     SubmissionStatusKind = "dropped"
	SubmissionStatusExpired     SubmissionStatusKind = "expired"
)

// String returns the wire-format value.
func (s SubmissionStatusKind) String() string { return string(s) }

// SubmissionStatusKindFromProto maps the protobuf enum onto the domain enum.
func SubmissionStatusKindFromProto(s oraclev1.SubmissionStatus_Status) SubmissionStatusKind {
	switch s {
	case oraclev1.SubmissionStatus_STATUS_PENDING:
		return SubmissionStatusPending
	case oraclev1.SubmissionStatus_STATUS_CONFIRMED:
		return SubmissionStatusConfirmed
	case oraclev1.SubmissionStatus_STATUS_FAILED:
		return SubmissionStatusFailed
	case oraclev1.SubmissionStatus_STATUS_DROPPED:
		return SubmissionStatusDropped
	case oraclev1.SubmissionStatus_STATUS_EXPIRED:
		return SubmissionStatusExpired
	case oraclev1.SubmissionStatus_STATUS_UNSPECIFIED:
		return SubmissionStatusUnspecified
	default:
		return SubmissionStatusUnspecified
	}
}

// SubmissionStatus is the REST projection of oracle.v1.SubmissionStatus.
// Numeric on-chain values (req_id, submitted_price) stay as base-10 decimal
// strings — they're uint256 / int256 and overflow Go integers.
type SubmissionStatus struct {
	ReqID          string               `json:"req_id"`
	AssetID        string               `json:"asset_id"`
	TxHash         string               `json:"tx_hash,omitempty"`
	SubmittedPrice string               `json:"submitted_price,omitempty"`
	SubmittedAt    *time.Time           `json:"submitted_at,omitempty"`
	Status         SubmissionStatusKind `json:"status"`
	RetryCount     uint32               `json:"retry_count"`
	LastError      string               `json:"last_error,omitempty"`
}

// SubmissionStatusFromProto converts oracle.v1.SubmissionStatus to the domain
// shape. A zero submitted_at surfaces as nil (JSON-omitted) so the frontend
// can distinguish "not yet broadcast" from "broadcast at epoch zero".
func SubmissionStatusFromProto(s *oraclev1.SubmissionStatus) SubmissionStatus {
	if s == nil {
		return SubmissionStatus{}
	}
	out := SubmissionStatus{
		ReqID:          s.GetReqId(),
		AssetID:        s.GetAssetId(),
		TxHash:         s.GetTxHash(),
		SubmittedPrice: s.GetSubmittedPrice(),
		Status:         SubmissionStatusKindFromProto(s.GetStatus()),
		RetryCount:     s.GetRetryCount(),
		LastError:      s.GetLastError(),
	}
	if ts := s.GetSubmittedAt(); ts != nil && (ts.GetSeconds() != 0 || ts.GetNanos() != 0) {
		t := ts.AsTime()
		out.SubmittedAt = &t
	}
	return out
}
