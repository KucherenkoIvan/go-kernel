// Package kafkacontracts declares the Kafka event contracts — the pairing
// of topic names with their protobuf message types. Producers and consumers
// in all services import these values, so topic and schema can never drift
// apart.
//
// One var per event, grouped by domain. The message types come from
// contracts/proto/events/ (buf-generated).
package kafkacontracts

import (
	eventsexamplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/events/example/v1"
	"github.com/KucherenkoIvan/go-kernel/kafka"
)

// example domain

var ExampleOrderPlaced = kafka.NewContract[*eventsexamplev1.OrderPlacedEvent]("example.order-placed.v1")
