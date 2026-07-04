package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

// Fail records the error and stops the handler chain; the error middleware
// writes the response. This is the single way handlers and middleware report
// failures:
//
//	if err != nil {
//		httpapi.Fail(c, err) // domain error, *HTTPError, or anything else
//		return
//	}
func Fail(c *gin.Context, err error) {
	_ = c.Error(err)
	c.Abort()
}

// requestIDMiddleware puts a request id (from the x-request-id header, or
// generated) into the request context and echoes it in the response header.
func requestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		if id := c.GetHeader(requestid.Header); id != "" {
			ctx = requestid.With(ctx, id)
		} else {
			ctx = requestid.Ensure(ctx)
		}
		c.Request = c.Request.WithContext(ctx)
		c.Header(requestid.Header, requestid.From(ctx))
		c.Next()
	}
}

// recoveryMiddleware converts panics into masked 500s.
func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in http handler",
					"method", c.Request.Method,
					"path", c.FullPath(),
					"requestId", requestid.From(c.Request.Context()),
					"panic", r,
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, internalErrorResponse(c))
			}
		}()
		c.Next()
	}
}

// errorMiddleware maps errors recorded via Fail into the uniform JSON error
// contract. Precedence:
//
//  1. *HTTPError            → its status and code
//  2. ddd domain error      → 400 (or the WithErrorStatus mapping), code = Error()
//  3. anything else         → 500 "internal_error", details logged, never sent
func errorMiddleware(logger *slog.Logger, statusFor func(code string) int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 || c.Writer.Written() {
			return
		}
		err := c.Errors.Last().Err
		log := logger.With(
			"method", c.Request.Method,
			"path", c.FullPath(),
			"requestId", requestid.From(c.Request.Context()),
		)

		var httpErr *HTTPError
		switch {
		case errors.As(err, &httpErr):
			log.Debug("http error", "code", httpErr.Code, "cause", httpErr.Unwrap())
			c.JSON(httpErr.Status, errorResponse(c, httpErr.Code))
		case ddd.IsDomainError(err):
			code := err.Error()
			log.Debug("domain error", "code", code)
			c.JSON(statusFor(code), errorResponse(c, code))
		default:
			log.Error("handler error", "error", err)
			c.JSON(http.StatusInternalServerError, internalErrorResponse(c))
		}
	}
}

func errorResponse(c *gin.Context, code string) ErrorResponse {
	return ErrorResponse{Error: ErrorBody{
		Code:      code,
		RequestID: requestid.From(c.Request.Context()),
	}}
}

func internalErrorResponse(c *gin.Context) ErrorResponse {
	return errorResponse(c, "internal_error")
}
