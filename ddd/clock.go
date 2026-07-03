package ddd

import "time"

// Clock is the port for obtaining the current time. Domain and application
// code that needs "now" takes a Clock instead of calling time.Now() — this
// keeps the logic deterministic in tests.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production Clock backed by time.Now().
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }
