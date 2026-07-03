package ddd

import "time"

// Event is a fact that happened in the domain. Immutable by convention.
// Features declare concrete events as type aliases:
//
//	type CouponUsedData struct {
//		CouponID CouponID
//		Code     string
//	}
//
//	type CouponUsedEvent = ddd.Event[CouponUsedData]
//
//	func NewCouponUsedEvent(data CouponUsedData) CouponUsedEvent {
//		return ddd.NewEvent("CouponUsedEvent", data)
//	}
type Event[T any] struct {
	ID         string
	Name       string
	OccurredAt time.Time
	Data       T
}

func (e Event[T]) EventID() string            { return e.ID }
func (e Event[T]) EventName() string          { return e.Name }
func (e Event[T]) EventOccurredAt() time.Time { return e.OccurredAt }

// DomainEvent is the envelope view of any Event[T] instantiation.
// Use it (or a narrower per-feature interface) as the element type of a
// heterogeneous event queue: EventRecorder[ddd.DomainEvent].
type DomainEvent interface {
	EventID() string
	EventName() string
	EventOccurredAt() time.Time
}

// NewEvent builds an event with a generated ID (UUIDv7) and OccurredAt = now.
func NewEvent[T any](name string, data T) Event[T] {
	return Event[T]{ID: NewEntityID(), Name: name, OccurredAt: time.Now(), Data: data}
}

// NewEventAt builds an event with an explicit ID and OccurredAt —
// useful in tests and when replaying events.
func NewEventAt[T any](name string, data T, id string, occurredAt time.Time) Event[T] {
	return Event[T]{ID: id, Name: name, OccurredAt: occurredAt, Data: data}
}
