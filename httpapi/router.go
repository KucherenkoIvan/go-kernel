// Package httpapi builds JSON REST APIs on Gin with the kernel's standard
// middleware chain — the HTTP mirror of grpckit:
//
//   - request-id: extracted from the x-request-id header or generated, put
//     into the request context (shared with grpckit — the id survives
//     HTTP → gRPC hops), echoed in the response header;
//   - panic recovery: 500 + log, never a crash;
//   - error mapping: ddd domain errors become 4xx with a machine-readable
//     code, everything else is masked as 500 — internals never leak.
//
// Containment rule: gin.Context never leaves adapters/httpapi of a feature.
// Handlers extract typed values and call use-cases with context.Context.
package httpapi

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

type routerConfig struct {
	logger        *slog.Logger
	errorStatuses map[string]int
}

// RouterOption customizes NewRouter.
type RouterOption func(*routerConfig)

// WithLogger sets the logger for handler errors and panics.
// Defaults to slog.Default().
func WithLogger(logger *slog.Logger) RouterOption {
	return func(c *routerConfig) { c.logger = logger }
}

// WithErrorStatus maps a domain error code to an HTTP status, e.g.
//
//	httpapi.WithErrorStatus("coupon_not_found", http.StatusNotFound)
//
// Unmapped domain errors default to 400.
func WithErrorStatus(domainCode string, status int) RouterOption {
	return func(c *routerConfig) { c.errorStatuses[domainCode] = status }
}

// NewRouter creates a *gin.Engine with the standard middleware chain
// installed. Mount feature routes on it (plain gin from here on — groups,
// custom middleware, the whole ecosystem), then start it with Run:
//
//	r := httpapi.NewRouter(httpapi.WithLogger(logger))
//	couponHandlers.RegisterRoutes(r.Group("/api"))
//	err := httpapi.Run(ctx, r, cfg.HTTPAddr)
func NewRouter(opts ...RouterOption) *gin.Engine {
	cfg := &routerConfig{
		logger:        slog.Default(),
		errorStatuses: map[string]int{},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	statusFor := func(code string) int {
		if s, ok := cfg.errorStatuses[code]; ok {
			return s
		}
		return http.StatusBadRequest
	}

	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(
		requestIDMiddleware(),
		recoveryMiddleware(cfg.logger),
		errorMiddleware(cfg.logger, statusFor),
	)
	return r
}
