// Package app runs a service: all adapters concurrently, shutdown on signal
// or first failure, cleanup in reverse registration order.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// Adapter is anything the service runs or owns: an HTTP/gRPC server, the
// event publisher, a relay worker, a DB pool. Both fields are optional:
// servers are Run-only, resources like the DB pool are Close-only.
type Adapter struct {
	// Name identifies the adapter in logs and error messages.
	Name string
	// Run blocks until ctx is cancelled (servers, workers). Returning an
	// error shuts the whole service down. Nil for resource-only adapters.
	Run func(ctx context.Context) error
	// Close releases resources. Called after every Run has returned, in
	// reverse registration order — register dependencies (db) before their
	// dependents (servers). Nil when there is nothing to release.
	Close func(ctx context.Context) error
}

// closeTimeout bounds the total cleanup phase.
const closeTimeout = 15 * time.Second

// Options configures RunWith. The zero value is Run's behavior.
type Options struct {
	// Logger receives the lifecycle lines (adapter started/stopped/closed,
	// the shutdown signal); nil is slog.Default().
	Logger *slog.Logger
}

// Run starts all adapters and blocks until SIGINT/SIGTERM, ctx cancellation,
// or the first Run error — then waits for every Run to return and calls
// Close in reverse registration order. Returns the joined run and close
// errors; nil on a clean shutdown.
//
// Shutdown is two-stage: once it has begun, for any of those reasons, the
// signal handler is released, so the next SIGINT/SIGTERM terminates the
// process with the signal's default action — the way out of a Run or Close
// that hangs. See RunWith for the logger option.
//
//	err := app.Run(ctx,
//		app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
//		app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close},
//		app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
//		app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
//	)
func Run(ctx context.Context, adapters ...Adapter) error {
	return RunWith(ctx, Options{}, adapters...)
}

// RunWith is Run configured by opts.
func RunWith(ctx context.Context, opts Options, adapters ...Adapter) error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Only the first signal is ours. signal.Stop restores the default action
	// unless the program registered its own handler for the signal, in which
	// case later signals go there.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gctx := errgroup.WithContext(ctx)
	// shutdown begun by ctx or an adapter failure releases the handler too
	releaseOnShutdown := context.AfterFunc(gctx, func() { signal.Stop(sigs) })
	defer releaseOnShutdown()
	go func() {
		select {
		case sig := <-sigs:
			signal.Stop(sigs)
			logger.Info("shutting down; signal again to force quit", "signal", sig.String())
			cancel()
		case <-gctx.Done():
		}
	}()

	for _, a := range adapters {
		if a.Run == nil {
			continue
		}
		g.Go(func() error {
			logger.Info("adapter started", "adapter", a.Name)
			if err := a.Run(gctx); err != nil {
				return fmt.Errorf("%s: %w", a.Name, err)
			}
			logger.Info("adapter stopped", "adapter", a.Name)
			return nil
		})
	}
	runErr := g.Wait()
	signal.Stop(sigs) // released before any Close, whatever ended the Run phase

	closeCtx, cancelClose := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancelClose()
	errs := []error{runErr}
	for i := len(adapters) - 1; i >= 0; i-- {
		a := adapters[i]
		if a.Close == nil {
			continue
		}
		if err := a.Close(closeCtx); err != nil {
			errs = append(errs, fmt.Errorf("closing %s: %w", a.Name, err))
		} else {
			logger.Info("adapter closed", "adapter", a.Name)
		}
	}
	return errors.Join(errs...)
}
