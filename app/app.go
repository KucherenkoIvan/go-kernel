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

// Run starts all adapters and blocks until SIGINT/SIGTERM, ctx cancellation,
// or the first Run error — then waits for every Run to return and calls
// Close in reverse registration order. Returns the joined run and close
// errors; nil on a clean shutdown.
//
//	err := app.Run(ctx,
//		app.Adapter{Name: "postgres", Close: func(context.Context) error { db.Close(); return nil }},
//		app.Adapter{Name: "events", Run: pub.Run, Close: pub.Close},
//		app.Adapter{Name: "http", Run: func(ctx context.Context) error { return httpapi.Run(ctx, r, cfg.HTTPAddr) }},
//		app.Adapter{Name: "grpc", Run: func(ctx context.Context) error { return grpckit.Run(ctx, srv, cfg.GRPCAddr) }},
//	)
func Run(ctx context.Context, adapters ...Adapter) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger := slog.Default()

	g, gctx := errgroup.WithContext(ctx)
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

	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
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
