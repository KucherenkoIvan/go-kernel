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

// handlerError logs and encodes the error a unary or streaming handler
// returned. An error that comes from the RPC's own context ending (the client
// cancelled, the deadline passed, the server closed the connection on
// shutdown) is reported as Canceled/DeadlineExceeded, not as a handler
// failure: a client cancel is routine and logged at debug; a deadline, or an
// Unavailable that may be a real outage, is logged at warn.
func handlerError(ctx context.Context, log *slog.Logger, err error, codeFor func(string) codes.Code) error {
	switch {
	case err == nil:
		return nil
	case ddd.IsDomainError(err):
		log.Debug("domain error", "code", err.Error())
		return encodeError(err, codeFor)
	case endedByContext(ctx, err):
		ended := status.FromContextError(ctx.Err())
		switch {
		case status.Code(err) == codes.Unavailable:
			// Send/Recv on a connection the server closed, or a downstream
			// outage that raced the cancel. Either way nobody is left to
			// receive it, so it is folded into the context's code; the warn
			// keeps a real outage visible.
			log.Warn("rpc unavailable after its context ended", "code", ended.Code(), "error", err)
		case ended.Code() == codes.DeadlineExceeded:
			log.Warn("rpc deadline exceeded", "error", err)
		default:
			log.Debug("rpc cancelled", "error", err)
		}
		return ended.Err()
	default:
		log.Error("handler error", "error", err)
		return encodeError(err, codeFor)
	}
}

// endedByContext reports whether err comes from the RPC's context ending:
// ctx.Err() itself (possibly wrapped), or what a stream's Send/Recv returns
// once the context is done — Canceled/DeadlineExceeded after a client cancel
// or deadline, Unavailable ("transport is closing") after the server closes
// the connection. Any other error is the handler's own, even after the end.
func endedByContext(ctx context.Context, err error) bool {
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
