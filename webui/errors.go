package webui

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/httpapi"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// ErrorPageData is what the error page template receives.
type ErrorPageData struct {
	Status    int
	Code      string
	RequestID string
}

type errorPagesConfig struct {
	logger        *slog.Logger
	errorStatuses map[string]int
}

// ErrorPagesOption customizes ErrorPages.
type ErrorPagesOption func(*errorPagesConfig)

// WithErrorStatus maps a domain error code to an HTTP status — the HTML
// twin of httpapi.WithErrorStatus. Unmapped domain errors default to 400.
func WithErrorStatus(domainCode string, status int) ErrorPagesOption {
	return func(c *errorPagesConfig) { c.errorStatuses[domainCode] = status }
}

// WithLogger sets the logger for handler errors. Defaults to slog.Default().
func WithLogger(logger *slog.Logger) ErrorPagesOption {
	return func(c *errorPagesConfig) { c.logger = logger }
}

// ErrorPages renders failures recorded via httpapi.Fail as an HTML page
// instead of the JSON contract. Scope it to the HTML route group — it writes
// the response first, so httpapi's global JSON middleware skips those
// requests while /api keeps its contract untouched:
//
//	web := r.Group("/", webui.ErrorPages(renderer, "error"))
//
// The same precedence as the JSON side: *httpapi.HTTPError → its status,
// ddd domain error → 400 (or the mapping), anything else → masked 500.
func ErrorPages(r *Renderer, page string, opts ...ErrorPagesOption) gin.HandlerFunc {
	cfg := &errorPagesConfig{
		logger:        slog.Default(),
		errorStatuses: map[string]int{},
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		log := cfg.logger.With(
			"method", c.Request.Method,
			"path", c.FullPath(),
			"requestId", requestid.From(c.Request.Context()),
		)

		status, code := http.StatusInternalServerError, "internal_error"
		var httpErr *httpapi.HTTPError
		switch {
		case errors.As(err, &httpErr):
			status, code = httpErr.Status, httpErr.Code
			log.Debug("http error", "code", code)
		case ddd.IsDomainError(err):
			code = err.Error()
			status = http.StatusBadRequest
			if s, ok := cfg.errorStatuses[code]; ok {
				status = s
			}
			log.Debug("domain error", "code", code)
		default:
			log.Error("handler error", "error", err)
		}

		r.Render(c, status, page, ErrorPageData{
			Status:    status,
			Code:      code,
			RequestID: requestid.From(c.Request.Context()),
		})
	}
}
