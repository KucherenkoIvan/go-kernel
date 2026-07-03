package ddd

// EventRecorder is an embeddable queue of domain events for aggregates.
//
//	type Coupon struct {
//		ddd.EventRecorder[CouponEvent]
//		state couponState
//	}
//
// PushEvent is called only from the aggregate's own methods; PopEvents is
// called in the application layer after the aggregate is persisted.
type EventRecorder[E any] struct {
	events []E
}

// PushEvent adds a domain event to the internal queue.
func (r *EventRecorder[E]) PushEvent(event E) {
	r.events = append(r.events, event)
}

// PopEvents returns the accumulated events and clears the queue —
// a repeated call returns an empty slice.
func (r *EventRecorder[E]) PopEvents() []E {
	events := r.events
	r.events = nil
	return events
}
