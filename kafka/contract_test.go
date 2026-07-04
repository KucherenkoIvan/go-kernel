package kafka_test

import (
	"testing"

	eventsexamplev1 "github.com/KucherenkoIvan/go-kernel/contracts/gen/events/example/v1"
	"github.com/KucherenkoIvan/go-kernel/kafka"
)

var orderPlaced = kafka.NewContract[*eventsexamplev1.OrderPlacedEvent]("example.order-placed.v1")

func TestContract_RoundTrip(t *testing.T) {
	payload, err := kafka.Encode(orderPlaced, &eventsexamplev1.OrderPlacedEvent{
		OrderId: "o-1", UserId: "u-1", TotalCents: 4200,
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := kafka.Decode(orderPlaced, payload)
	if err != nil {
		t.Fatal(err)
	}
	if msg.GetOrderId() != "o-1" || msg.GetUserId() != "u-1" || msg.GetTotalCents() != 4200 {
		t.Fatalf("roundtrip mismatch: %+v", msg)
	}
}

func TestContract_DecodeGarbage(t *testing.T) {
	if _, err := kafka.Decode(orderPlaced, []byte{0xff, 0xff, 0xff}); err == nil {
		t.Fatal("garbage must not decode")
	}
}
