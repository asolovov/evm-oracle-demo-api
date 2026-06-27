package models

import (
	"time"

	indexerv1 "github.com/asolovov/evm-oracle-demo-api/internal/genproto/indexer/v1"
)

// EventKind discriminates the contract-emitted events the indexer surfaces.
type EventKind string

// EventKind* — the contract-emitted event discriminators the BFF understands.
const (
	EventKindUnspecified     EventKind = ""
	EventKindPriceRequested  EventKind = "PRICE_REQUESTED"
	EventKindPriceFulfilled  EventKind = "PRICE_FULFILLED"
	EventKindAssetRegistered EventKind = "ASSET_REGISTERED"
)

// String returns the wire-format value.
func (k EventKind) String() string { return string(k) }

// EventKindFromProto maps the protobuf enum onto the domain enum.
func EventKindFromProto(k indexerv1.EventKind) EventKind {
	switch k {
	case indexerv1.EventKind_EVENT_KIND_PRICE_REQUESTED:
		return EventKindPriceRequested
	case indexerv1.EventKind_EVENT_KIND_PRICE_FULFILLED:
		return EventKindPriceFulfilled
	case indexerv1.EventKind_EVENT_KIND_ASSET_REGISTERED:
		return EventKindAssetRegistered
	case indexerv1.EventKind_EVENT_KIND_UNSPECIFIED:
		return EventKindUnspecified
	default:
		return EventKindUnspecified
	}
}

// EventKindToProto inverts EventKindFromProto for the gRPC client wrappers.
func EventKindToProto(k EventKind) indexerv1.EventKind {
	switch k {
	case EventKindPriceRequested:
		return indexerv1.EventKind_EVENT_KIND_PRICE_REQUESTED
	case EventKindPriceFulfilled:
		return indexerv1.EventKind_EVENT_KIND_PRICE_FULFILLED
	case EventKindAssetRegistered:
		return indexerv1.EventKind_EVENT_KIND_ASSET_REGISTERED
	case EventKindUnspecified:
		return indexerv1.EventKind_EVENT_KIND_UNSPECIFIED
	default:
		return indexerv1.EventKind_EVENT_KIND_UNSPECIFIED
	}
}

// EventMeta carries the chain-derived context for every observed event.
type EventMeta struct {
	ContractAddress string    `json:"contract_address"`
	TxHash          string    `json:"tx_hash"`
	BlockHash       string    `json:"block_hash"`
	BlockNumber     uint64    `json:"block_number"`
	LogIndex        uint32    `json:"log_index"`
	ObservedAt      time.Time `json:"observed_at"`
	Confirmations   uint32    `json:"confirmations"`
}

// Event is the WS / read-side projection of one observed chain event.
// Exactly one of PriceRequested / PriceFulfilled / AssetRegistered is set
// per the Kind discriminator.
type Event struct {
	Meta            EventMeta             `json:"meta"`
	Kind            EventKind             `json:"kind"`
	PriceRequested  *PriceRequestedEvent  `json:"price_requested,omitempty"`
	PriceFulfilled  *PriceFulfilledEvent  `json:"price_fulfilled,omitempty"`
	AssetRegistered *AssetRegisteredEvent `json:"asset_registered,omitempty"`
}

// PriceRequestedEvent matches indexer.v1.PriceRequestedEvent.
type PriceRequestedEvent struct {
	ReqID     string `json:"req_id"`
	AssetID   string `json:"asset_id"`
	Requester string `json:"requester"`
}

// PriceFulfilledEvent matches indexer.v1.PriceFulfilledEvent. Numeric fields
// stay as base-10 decimal strings — uint256 / int256 don't survive a Go
// numeric round-trip and the frontend formats them as strings anyway.
type PriceFulfilledEvent struct {
	ReqID     string `json:"req_id"`
	AssetID   string `json:"asset_id"`
	Price     string `json:"price"`
	Timestamp string `json:"timestamp"`
	RoundID   string `json:"round_id"`
}

// AssetRegisteredEvent matches indexer.v1.AssetRegisteredEvent.
type AssetRegisteredEvent struct {
	AssetID    string `json:"asset_id"`
	Aggregator string `json:"aggregator"`
}

// EventFromProto converts an indexer.v1.Event into the WS-shaped domain
// type. Nil-safe — returns the zero value for nil input.
func EventFromProto(e *indexerv1.Event) Event {
	if e == nil {
		return Event{}
	}
	out := Event{
		Meta: eventMetaFromProto(e.GetMeta()),
		Kind: EventKindFromProto(e.GetKind()),
	}
	switch p := e.GetPayload().(type) {
	case *indexerv1.Event_PriceRequested:
		if p != nil && p.PriceRequested != nil {
			out.PriceRequested = &PriceRequestedEvent{
				ReqID:     p.PriceRequested.GetReqId(),
				AssetID:   p.PriceRequested.GetAssetId(),
				Requester: p.PriceRequested.GetRequester(),
			}
		}
	case *indexerv1.Event_PriceFulfilled:
		if p != nil && p.PriceFulfilled != nil {
			out.PriceFulfilled = &PriceFulfilledEvent{
				ReqID:     p.PriceFulfilled.GetReqId(),
				AssetID:   p.PriceFulfilled.GetAssetId(),
				Price:     p.PriceFulfilled.GetPrice(),
				Timestamp: p.PriceFulfilled.GetTimestamp(),
				RoundID:   p.PriceFulfilled.GetRoundId(),
			}
		}
	case *indexerv1.Event_AssetRegistered:
		if p != nil && p.AssetRegistered != nil {
			out.AssetRegistered = &AssetRegisteredEvent{
				AssetID:    p.AssetRegistered.GetAssetId(),
				Aggregator: p.AssetRegistered.GetAggregator(),
			}
		}
	}
	return out
}

func eventMetaFromProto(m *indexerv1.EventMeta) EventMeta {
	if m == nil {
		return EventMeta{}
	}
	return EventMeta{
		ContractAddress: m.GetContractAddress(),
		TxHash:          m.GetTxHash(),
		BlockHash:       m.GetBlockHash(),
		BlockNumber:     m.GetBlockNumber(),
		LogIndex:        m.GetLogIndex(),
		ObservedAt:      m.GetObservedAt().AsTime(),
		Confirmations:   m.GetConfirmations(),
	}
}
