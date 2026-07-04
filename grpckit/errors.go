package grpckit

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/KucherenkoIvan/go-kernel/ddd"
)

// errorInfoDomain marks an ErrorInfo detail as carrying a ddd domain error.
const errorInfoDomain = "ddd"

// RemoteDomainError is a domain error received over gRPC. It embeds
// ddd.DomainError, so ddd.IsDomainError keeps working across service
// boundaries; callers branch on Code:
//
//	var remote *grpckit.RemoteDomainError
//	if errors.As(err, &remote) && remote.Code == "coupon_exhausted" { ... }
type RemoteDomainError struct {
	ddd.DomainError
	Code   string // the domain error code, e.g. "coupon_exhausted"
	Status *status.Status
}

func (e *RemoteDomainError) Error() string { return e.Code }

// GRPCStatus makes status.FromError / status.Code see through this error.
func (e *RemoteDomainError) GRPCStatus() *status.Status { return e.Status }

// encodeError converts a handler error into a gRPC status. A domain error
// becomes a status with an ErrorInfo detail carrying its code (by the ddd
// convention the code is Error()). Anything else is masked as codes.Internal
// so internals never leak to clients; existing statuses pass through.
func encodeError(err error, codeFor func(code string) codes.Code) error {
	if err == nil {
		return nil
	}

	if ddd.IsDomainError(err) {
		code := err.Error()
		st := status.New(codeFor(code), code)
		if detailed, dErr := st.WithDetails(&errdetails.ErrorInfo{
			Reason: code,
			Domain: errorInfoDomain,
		}); dErr == nil {
			st = detailed
		}
		return st.Err()
	}

	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Internal, "internal error")
}

// decodeError restores a RemoteDomainError from a status produced by
// encodeError; other errors are returned as-is.
func decodeError(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == errorInfoDomain {
			return &RemoteDomainError{Code: info.Reason, Status: st}
		}
	}
	return err
}
