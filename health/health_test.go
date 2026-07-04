package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/KucherenkoIvan/go-kernel/grpckit"
	"github.com/KucherenkoIvan/go-kernel/grpckit/grpckittest"
	"github.com/KucherenkoIvan/go-kernel/health"
)

func pass(context.Context) error { return nil }
func fail(context.Context) error { return errors.New("connection refused") }

func TestHandler_AllOK(t *testing.T) {
	checker := health.NewChecker(
		health.Check{Name: "postgres", Check: pass},
		health.Check{Name: "kafka", Check: pass},
	)

	w := httptest.NewRecorder()
	checker.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var report health.Report
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != health.StatusOK || len(report.Checks) != 2 {
		t.Fatalf("report = %+v", report)
	}
	// the 200 body still lists every registered check with its positive status
	for name, result := range report.Checks {
		if result.Status != health.StatusOK || result.Error != "" {
			t.Fatalf("check %s in a 200 report: %+v", name, result)
		}
	}
}

func TestHandler_DegradedWithDetails(t *testing.T) {
	checker := health.NewChecker(
		health.Check{Name: "postgres", Check: fail},
		health.Check{Name: "kafka", Check: pass},
	)

	w := httptest.NewRecorder()
	checker.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", w.Code)
	}
	var report health.Report
	_ = json.Unmarshal(w.Body.Bytes(), &report)
	if report.Status != health.StatusDegraded {
		t.Fatalf("report = %+v", report)
	}
	if report.Checks["postgres"].Error != "connection refused" || report.Checks["kafka"].Status != health.StatusOK {
		t.Fatalf("details missing: %+v", report.Checks)
	}
}

func TestLiveness_AlwaysOK(t *testing.T) {
	w := httptest.NewRecorder()
	health.Liveness().ServeHTTP(w, httptest.NewRequest("GET", "/livez", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestRegisterGRPC_ReflectsAggregate(t *testing.T) {
	srv := grpckit.NewServer()
	health.NewChecker(health.Check{Name: "postgres", Check: fail}).RegisterGRPC(srv)
	client := grpc_health_v1.NewHealthClient(grpckittest.Serve(t, srv))

	resp, err := client.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf("status = %v, want NOT_SERVING", resp.GetStatus())
	}
}
