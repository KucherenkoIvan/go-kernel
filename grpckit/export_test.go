package grpckit

import (
	"testing"
	"time"
)

// SetShutdownTimeout shrinks the graceful-stop bound for one test.
func SetShutdownTimeout(tb testing.TB, d time.Duration) {
	old := shutdownTimeout
	shutdownTimeout = d
	tb.Cleanup(func() { shutdownTimeout = old })
}
