package models

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	indexerv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/indexer/v1"
	oraclev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/oracle/v1"
	pricev1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/price/v1"
)

func TestAggregatedPriceFromProto(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	fetched := now.Add(-30 * time.Second)
	observed := now.Add(-45 * time.Second)

	in := &pricev1.AggregatedPrice{
		AssetId:      "weth",
		MedianPrice:  3450.20,
		AggregatedAt: timestamppb.New(now),
		Sources: []*pricev1.SourceContribution{
			{
				Source:           "coingecko",
				Price:            3451.10,
				FetchedAt:        timestamppb.New(fetched),
				SourceObservedAt: timestamppb.New(observed),
				AgeSec:           45,
				Included:         true,
			},
			{
				Source:   "binance",
				Price:    3449.80,
				Included: false,
			},
		},
	}

	got := AggregatedPriceFromProto(in)
	if got.AssetID != "weth" {
		t.Fatalf("AssetID = %q, want %q", got.AssetID, "weth")
	}
	if got.MedianPrice != 3450.20 {
		t.Fatalf("MedianPrice = %v, want %v", got.MedianPrice, 3450.20)
	}
	if !got.AggregatedAt.Equal(now) {
		t.Fatalf("AggregatedAt = %v, want %v", got.AggregatedAt, now)
	}
	if len(got.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(got.Sources))
	}
	if got.Sources[0].Source != "coingecko" || !got.Sources[0].Included {
		t.Fatalf("Sources[0] = %+v", got.Sources[0])
	}
	if got.Sources[1].Included {
		t.Fatalf("Sources[1].Included = true, want false")
	}

	// nil input -> zero value, no panic.
	if zero := AggregatedPriceFromProto(nil); zero.AssetID != "" {
		t.Fatalf("nil input should yield zero value, got %+v", zero)
	}
}

func TestEventFromProtoDispatchesEachKind(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	meta := &indexerv1.EventMeta{
		ContractAddress: "0xagg",
		TxHash:          "0xtx",
		BlockHash:       "0xblock",
		BlockNumber:     100,
		LogIndex:        3,
		ObservedAt:      timestamppb.New(now),
		Confirmations:   7,
	}

	requested := &indexerv1.Event{
		Meta: meta,
		Kind: indexerv1.EventKind_EVENT_KIND_PRICE_REQUESTED,
		Payload: &indexerv1.Event_PriceRequested{
			PriceRequested: &indexerv1.PriceRequestedEvent{
				ReqId:     "42",
				AssetId:   "weth",
				Requester: "0xrequester",
			},
		},
	}
	gotReq := EventFromProto(requested)
	if gotReq.Kind != EventKindPriceRequested {
		t.Fatalf("Kind = %q, want %q", gotReq.Kind, EventKindPriceRequested)
	}
	if gotReq.PriceRequested == nil || gotReq.PriceRequested.ReqID != "42" {
		t.Fatalf("PriceRequested mismatch: %+v", gotReq.PriceRequested)
	}
	if gotReq.PriceFulfilled != nil || gotReq.AssetRegistered != nil {
		t.Fatalf("expected only PriceRequested payload to be set")
	}

	fulfilled := &indexerv1.Event{
		Meta: meta,
		Kind: indexerv1.EventKind_EVENT_KIND_PRICE_FULFILLED,
		Payload: &indexerv1.Event_PriceFulfilled{
			PriceFulfilled: &indexerv1.PriceFulfilledEvent{
				ReqId:     "42",
				AssetId:   "weth",
				Price:     "345020000000",
				Timestamp: "1748345200",
				RoundId:   "7",
			},
		},
	}
	gotFul := EventFromProto(fulfilled)
	if gotFul.Kind != EventKindPriceFulfilled || gotFul.PriceFulfilled.Price != "345020000000" {
		t.Fatalf("PriceFulfilled mismatch: %+v", gotFul.PriceFulfilled)
	}

	registered := &indexerv1.Event{
		Meta: meta,
		Kind: indexerv1.EventKind_EVENT_KIND_ASSET_REGISTERED,
		Payload: &indexerv1.Event_AssetRegistered{
			AssetRegistered: &indexerv1.AssetRegisteredEvent{
				AssetId:    "weth",
				Aggregator: "0xagg",
			},
		},
	}
	gotReg := EventFromProto(registered)
	if gotReg.Kind != EventKindAssetRegistered || gotReg.AssetRegistered.Aggregator != "0xagg" {
		t.Fatalf("AssetRegistered mismatch: %+v", gotReg.AssetRegistered)
	}
}

func TestEventKindRoundTrip(t *testing.T) {
	for _, k := range []EventKind{EventKindPriceRequested, EventKindPriceFulfilled, EventKindAssetRegistered} {
		got := EventKindFromProto(EventKindToProto(k))
		if got != k {
			t.Fatalf("round-trip: %q -> %q", k, got)
		}
	}
}

func TestRequestSummaryFromProto(t *testing.T) {
	requested := time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)
	fulfilled := requested.Add(45 * time.Second)

	in := &indexerv1.RequestStatus{
		ReqId:           "42",
		AssetId:         "weth",
		Status:          indexerv1.RequestStatus_STATUS_FULFILLED,
		Requester:       "0xreq",
		RequestedTxHash: "0xtx_req",
		FulfilledTxHash: "0xtx_ful",
		FulfilledPrice:  "345020000000",
		RequestedAt:     timestamppb.New(requested),
		FulfilledAt:     timestamppb.New(fulfilled),
	}
	got := RequestSummaryFromProto(in)
	if got.Status != RequestStatusFulfilled {
		t.Fatalf("Status = %q, want %q", got.Status, RequestStatusFulfilled)
	}
	if got.RequestedAt == nil || !got.RequestedAt.Equal(requested) {
		t.Fatalf("RequestedAt = %v, want %v", got.RequestedAt, requested)
	}
	if got.FulfilledAt == nil || !got.FulfilledAt.Equal(fulfilled) {
		t.Fatalf("FulfilledAt = %v, want %v", got.FulfilledAt, fulfilled)
	}

	// Empty fulfilled timestamp surfaces as nil.
	pending := &indexerv1.RequestStatus{
		ReqId:   "43",
		AssetId: "weth",
		Status:  indexerv1.RequestStatus_STATUS_PENDING,
	}
	if got := RequestSummaryFromProto(pending); got.FulfilledAt != nil {
		t.Fatalf("FulfilledAt should be nil for pending request, got %v", got.FulfilledAt)
	}
}

func TestSubmissionStatusFromProto(t *testing.T) {
	submitted := time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC)
	in := &oraclev1.SubmissionStatus{
		ReqId:          "42",
		AssetId:        "weth",
		TxHash:         "0xfeed",
		SubmittedPrice: "345020000000",
		SubmittedAt:    timestamppb.New(submitted),
		Status:         oraclev1.SubmissionStatus_STATUS_EXPIRED,
		RetryCount:     2,
		LastError:      "ttl elapsed before broadcast",
	}
	got := SubmissionStatusFromProto(in)
	if got.Status != SubmissionStatusExpired {
		t.Fatalf("Status = %q, want %q", got.Status, SubmissionStatusExpired)
	}
	if got.ReqID != "42" || got.SubmittedPrice != "345020000000" || got.RetryCount != 2 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.SubmittedAt == nil || !got.SubmittedAt.Equal(submitted) {
		t.Fatalf("SubmittedAt = %v, want %v", got.SubmittedAt, submitted)
	}

	// Every proto enum value maps to a non-empty domain kind except UNSPECIFIED.
	for proto, want := range map[oraclev1.SubmissionStatus_Status]SubmissionStatusKind{
		oraclev1.SubmissionStatus_STATUS_PENDING:   SubmissionStatusPending,
		oraclev1.SubmissionStatus_STATUS_CONFIRMED: SubmissionStatusConfirmed,
		oraclev1.SubmissionStatus_STATUS_FAILED:    SubmissionStatusFailed,
		oraclev1.SubmissionStatus_STATUS_DROPPED:   SubmissionStatusDropped,
		oraclev1.SubmissionStatus_STATUS_EXPIRED:   SubmissionStatusExpired,
	} {
		if got := SubmissionStatusKindFromProto(proto); got != want {
			t.Fatalf("SubmissionStatusKindFromProto(%v) = %q, want %q", proto, got, want)
		}
	}

	// Zero submitted_at -> nil.
	pending := &oraclev1.SubmissionStatus{ReqId: "0", AssetId: "weth", Status: oraclev1.SubmissionStatus_STATUS_PENDING}
	if got := SubmissionStatusFromProto(pending); got.SubmittedAt != nil {
		t.Fatalf("SubmittedAt should be nil when unset, got %v", got.SubmittedAt)
	}
}
