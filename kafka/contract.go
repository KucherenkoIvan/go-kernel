// Package kafka provides typed, contract-first Kafka messaging on franz-go:
// producers and consumer groups bound to protobuf contracts, with request-id
// propagation through headers.
//
// A contract pairs a topic with its protobuf message type — producer and
// consumers agree on both by importing the same contract value:
//
//	// contracts/kafkacontracts/example.go (shared kernel)
//	var OrderPlaced = kafka.NewContract[*eventsv1.OrderPlacedEvent]("example.order-placed.v1")
package kafka

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Contract binds a topic to its protobuf message type. Transport is
// protobuf-only by design (contract-first, like the gRPC side).
type Contract[T proto.Message] struct {
	Topic string
}

// NewContract declares a contract. Topic naming convention:
// "<domain>.<event>.<version>", e.g. "example.order-placed.v1".
func NewContract[T proto.Message](topic string) Contract[T] {
	return Contract[T]{Topic: topic}
}

// Encode serializes a message for the contract's topic.
func Encode[T proto.Message](c Contract[T], msg T) ([]byte, error) {
	payload, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("kafka: encoding %s: %w", c.Topic, err)
	}
	return payload, nil
}

// Decode deserializes a payload from the contract's topic.
func Decode[T proto.Message](c Contract[T], payload []byte) (T, error) {
	var zero T
	msg := zero.ProtoReflect().Type().New().Interface().(T) //nolint:errcheck // constructed from T itself
	if err := proto.Unmarshal(payload, msg); err != nil {
		return zero, fmt.Errorf("kafka: decoding %s: %w", c.Topic, err)
	}
	return msg, nil
}
