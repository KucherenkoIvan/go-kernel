// Package health provides self-diagnosing health checks: every infra adapter
// registers a named check, and the service reports per-dependency status
// with details — not just "the pod is reachable".
//
// Liveness and readiness are deliberately separate: /livez only says the
// process is up (a failing dependency must NOT make the orchestrator restart
// pods — that fixes nothing), while /healthz runs the deep checks and is the
// one to wire as the readiness probe.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Check is one named dependency diagnosis, e.g.
//
//	health.Check{Name: "postgres", Check: db.Ping}
type Check struct {
	Name  string
	Check func(ctx context.Context) error
}

// checkTimeout bounds each individual check.
const checkTimeout = 5 * time.Second

const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// Report is the aggregate diagnosis.
type Report struct {
	Status string                 `json:"status"` // "ok" | "degraded"
	Checks map[string]CheckResult `json:"checks"`
}

type CheckResult struct {
	Status string `json:"status"`          // "ok" | "down"
	Error  string `json:"error,omitempty"` // present when down
}

// Checker runs the registered checks in parallel with a per-check timeout.
type Checker struct {
	checks []Check
}

func NewChecker(checks ...Check) *Checker {
	return &Checker{checks: checks}
}

// Run diagnoses all dependencies.
func (c *Checker) Run(ctx context.Context) Report {
	report := Report{Status: StatusOK, Checks: make(map[string]CheckResult, len(c.checks))}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, check := range c.checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()

			result := CheckResult{Status: StatusOK}
			if err := check.Check(checkCtx); err != nil {
				result = CheckResult{Status: "down", Error: err.Error()}
			}

			mu.Lock()
			defer mu.Unlock()
			report.Checks[check.Name] = result
			if result.Status != StatusOK {
				report.Status = StatusDegraded
			}
		}()
	}
	wg.Wait()
	return report
}

// Handler serves the detailed diagnosis: 200 when all checks pass,
// 503 + the same JSON report when degraded. Wire it as the readiness probe.
//
//	r.GET("/healthz", gin.WrapH(checker.Handler()))
func (c *Checker) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		report := c.Run(r.Context())
		status := http.StatusOK
		if report.Status != StatusOK {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(report)
	})
}

// Liveness always answers 200 — "the process is up". Wire it as the
// liveness probe.
//
//	r.GET("/livez", gin.WrapH(health.Liveness()))
func Liveness() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}
