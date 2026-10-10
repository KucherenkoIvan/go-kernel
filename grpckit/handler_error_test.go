package grpckit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

type notFoundError struct{ ddd.DomainError }

func (e *notFoundError) Error() string { return "thing_not_found" }

func TestHandlerError_CodesAndLogLevels(t *testing.T) {
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()

	transportClosing := status.Error(codes.Unavailable, "transport is closing")

	tests := []struct {
		name     string
		ctx      context.Context
		err      error
		wantCode codes.Code
		wantMsg  string // the status message the client gets
		level    string
		log      string
	}{
		{"domain error", live, &notFoundError{}, codes.FailedPrecondition, "thing_not_found", "DEBUG", "domain error"},
		{"domain error wins after the end", cancelled, &notFoundError{}, codes.FailedPrecondition, "thing_not_found", "DEBUG", "domain error"},
		{"ctx.Err after cancel", cancelled, context.Canceled, codes.Canceled, "context canceled", "DEBUG", "rpc cancelled"},
		{"wrapped ctx.Err after cancel", cancelled, fmt.Errorf("query: %w", context.Canceled), codes.Canceled, "context canceled", "DEBUG", "rpc cancelled"},
		{"Send status after cancel", cancelled, status.Error(codes.Canceled, "context canceled"), codes.Canceled, "context canceled", "DEBUG", "rpc cancelled"},
		{"ctx.Err after deadline", expired, context.DeadlineExceeded, codes.DeadlineExceeded, "context deadline exceeded", "WARN", "rpc deadline exceeded"},
		{"Unavailable after cancel", cancelled, transportClosing, codes.Canceled, "context canceled", "WARN", "rpc unavailable after its context ended"},
		{"Unavailable after deadline", expired, transportClosing, codes.DeadlineExceeded, "context deadline exceeded", "WARN", "rpc unavailable after its context ended"},
		{"plain error after the end", cancelled, errors.New("db down"), codes.Internal, "internal error", "ERROR", "handler error"},
		{"plain error", live, errors.New("db down"), codes.Internal, "internal error", "ERROR", "handler error"},
		{"Canceled from elsewhere, ctx live", live, context.Canceled, codes.Internal, "internal error", "ERROR", "handler error"},
		{"status passes through", live, transportClosing, codes.Unavailable, "transport is closing", "ERROR", "handler error"},
		{"wrapped status passes through with the wrapping text", live,
			fmt.Errorf("lookup: %w", status.Error(codes.NotFound, "no row")),
			codes.NotFound, "lookup: rpc error: code = NotFound desc = no row", "ERROR", "handler error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

			got := status.Convert(handlerError(tt.ctx, log, tt.err, func(string) codes.Code { return codes.FailedPrecondition }))
			if got.Code() != tt.wantCode || got.Message() != tt.wantMsg {
				t.Errorf("client gets %v %q, want %v %q", got.Code(), got.Message(), tt.wantCode, tt.wantMsg)
			}

			var record struct{ Level, Msg string }
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
				t.Fatalf("want exactly one log record, got %q: %v", logs.String(), err)
			}
			if record.Level != tt.level || record.Msg != tt.log {
				t.Errorf("logged %s %q, want %s %q", record.Level, record.Msg, tt.level, tt.log)
			}
		})
	}
}
