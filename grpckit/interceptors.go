package grpckit

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// serverInterceptor is the standard unary server chain: request-id, panic
// recovery, error logging, domain-error encoding.
func serverInterceptor(logger *slog.Logger, codeFor func(string) codes.Code) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp any, err error) {
		ctx = ensureRequestID(ctx)
		log := logger.With("method", info.FullMethod, "requestId", RequestID(ctx))
		defer recoverPanic(log, &err)

		resp, err = handler(ctx, req)
		return resp, handlerError(ctx, log, err, codeFor)
	}
}

// streamServerInterceptor is serverInterceptor for streaming RPCs: the
// handler and every interceptor after it see a stream whose Context carries
// the request id; the error the handler returns is logged and encoded the
// same way.
func streamServerInterceptor(logger *slog.Logger, codeFor func(string) codes.Code) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (err error) {
		ctx := ensureRequestID(ss.Context())
		log := logger.With("method", info.FullMethod, "requestId", RequestID(ctx))
		defer recoverPanic(log, &err)

		err = handler(srv, StreamWithContext(ctx, ss))
		return handlerError(ctx, log, err, codeFor)
	}
}

// recoverPanic turns a panic in the handler (or an interceptor after the
// standard one) into a masked Internal. Only the handler's own goroutine is
// covered: a panic in a goroutine the handler starts still crashes the process.
func recoverPanic(log *slog.Logger, err *error) {
	if r := recover(); r != nil {
		log.Error("panic in grpc handler", "panic", r, "stack", string(debug.Stack()))
		*err = status.Error(codes.Internal, "internal error")
	}
}

// handlerError logs and encodes the error a handler returned. An error caused
// by the RPC's own cancellation (the client went away, the deadline passed,
// the server is shutting down) is routine for a streaming RPC: it becomes
// Canceled/DeadlineExceeded and is logged at debug, not as a handler failure.
func handlerError(ctx context.Context, log *slog.Logger, err error, codeFor func(string) codes.Code) error {
	switch {
	case err == nil:
		return nil
	case ddd.IsDomainError(err):
		log.Debug("domain error", "code", err.Error())
		return encodeError(err, codeFor)
	case causedByCancel(ctx, err):
		log.Debug("rpc ended by context", "error", err)
		return status.FromContextError(ctx.Err()).Err()
	default:
		log.Error("handler error", "error", err)
		return encodeError(err, codeFor)
	}
}

// causedByCancel reports whether err is the RPC context's own end: ctx.Err()
// itself, or what a stream's Send/Recv returns once the context is done —
// Canceled/DeadlineExceeded after a client cancel or deadline, Unavailable
// ("transport is closing") after the server closes the connection.
func causedByCancel(ctx context.Context, err error) bool {
	ctxErr := ctx.Err()
	if ctxErr == nil {
		return false
	}
	if errors.Is(err, ctxErr) {
		return true
	}
	switch status.Code(err) {
	case codes.Canceled, codes.DeadlineExceeded, codes.Unavailable:
		return true
	}
	return false
}

// clientInterceptor is the standard unary client chain: request-id
// propagation and domain-error decoding.
func clientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = injectRequestID(ctx)
		return decodeError(invoker(ctx, method, req, reply, cc, opts...))
	}
}

// streamClientInterceptor is clientInterceptor for streaming RPCs: the
// request id goes out with the stream's headers, and the status a stream ends
// with is decoded on RecvMsg (where generated clients surface it), so a
// domain error the handler returned arrives as a RemoteDomainError.
func streamClientInterceptor() grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		cs, err := streamer(injectRequestID(ctx), desc, cc, method, opts...)
		if err != nil {
			return nil, decodeError(err)
		}
		return &decodingClientStream{ClientStream: cs}, nil
	}
}

// decodingClientStream decodes the errors a ClientStream returns; io.EOF (the
// clean end of a stream) passes through untouched.
type decodingClientStream struct {
	grpc.ClientStream
}

func (s *decodingClientStream) RecvMsg(m any) error {
	return decodeError(s.ClientStream.RecvMsg(m))
}

func (s *decodingClientStream) SendMsg(m any) error {
	return decodeError(s.ClientStream.SendMsg(m))
}
