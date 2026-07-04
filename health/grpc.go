package health

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// RegisterGRPC exposes the checker as the standard gRPC health service.
// The protocol carries only SERVING / NOT_SERVING — the detailed report
// lives on the HTTP endpoint; gRPC reflects the aggregate.
func (c *Checker) RegisterGRPC(srv *grpc.Server) {
	grpc_health_v1.RegisterHealthServer(srv, &grpcHealth{checker: c})
}

type grpcHealth struct {
	grpc_health_v1.UnimplementedHealthServer
	checker *Checker
}

func (h *grpcHealth) Check(ctx context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if h.checker.Run(ctx).Status != StatusOK {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}
	return &grpc_health_v1.HealthCheckResponse{Status: status}, nil
}
