package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// shutdownTimeout bounds how long in-flight requests may finish after the
// context is cancelled.
const shutdownTimeout = 10 * time.Second

// Run listens on addr and serves until ctx is cancelled, then shuts down
// gracefully. Blocks; returns nil on graceful shutdown.
func Run(ctx context.Context, handler http.Handler, addr string) error {
	srv := &http.Server{Addr: addr, Handler: handler}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err // immediate failure: port busy, bad addr
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
