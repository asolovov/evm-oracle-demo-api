// Package wshub fans the BFF's two upstream gRPC streams (price.Subscribe +
// indexer.StreamEvents) out to a single multiplexed WebSocket endpoint at
// /ws/stream. The hub also keeps the aggregator registry fresh by forwarding
// live AssetRegistered events into it.
package wshub

import (
	"encoding/json"

	"github.com/asolovov/evm-oracle-demo-api/internal/models"
)

// MessageType discriminates the WS envelope. Clients dispatch on this field.
type MessageType string

const (
	// MessageTypePrice carries a models.AggregatedPrice payload.
	MessageTypePrice MessageType = "price"
	// MessageTypeEvent carries a models.Event payload.
	MessageTypeEvent MessageType = "event"
)

// WireMessage is the JSON envelope every WS frame carries. Type drives the
// dispatch on the receiving end so the frontend can wire a single subscriber
// to both streams.
type WireMessage struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// MarshalPrice serializes an AggregatedPrice into the wire envelope.
func MarshalPrice(p models.AggregatedPrice) ([]byte, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WireMessage{Type: MessageTypePrice, Payload: payload})
}

// MarshalEvent serializes an Event into the wire envelope.
func MarshalEvent(e models.Event) ([]byte, error) {
	payload, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WireMessage{Type: MessageTypeEvent, Payload: payload})
}
