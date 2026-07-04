package httpapi_test

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/KucherenkoIvan/go-kernel/auth"
	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/httpapi"
	"github.com/KucherenkoIvan/go-kernel/requestid"
)

type couponExhaustedError struct{ ddd.DomainError }

func (e *couponExhaustedError) Error() string { return "coupon_exhausted" }

type couponNotFoundError struct{ ddd.DomainError }

func (e *couponNotFoundError) Error() string { return "coupon_not_found" }

func newRouter(t *testing.T) *gin.Engine {
	t.Helper()
	r := httpapi.NewRouter(
		httpapi.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		httpapi.WithErrorStatus("coupon_not_found", http.StatusNotFound),
	)
	r.GET("/ok", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"requestId": requestid.From(c.Request.Context())})
	})
	r.GET("/domain-error", func(c *gin.Context) { httpapi.Fail(c, &couponExhaustedError{}) })
	r.GET("/mapped-error", func(c *gin.Context) { httpapi.Fail(c, &couponNotFoundError{}) })
	r.GET("/http-error", func(c *gin.Context) {
		httpapi.Fail(c, httpapi.BadRequest("invalid_request", errors.New("field x is required")))
	})
	r.GET("/internal", func(c *gin.Context) {
		httpapi.Fail(c, errors.New("connection refused to 10.0.0.5:5432"))
	})
	r.GET("/panic", func(c *gin.Context) { panic("boom") })
	return r
}

func do(t *testing.T, r *gin.Engine, method, path string, header map[string]string) (*httptest.ResponseRecorder, httpapi.ErrorResponse) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body httpapi.ErrorResponse
	if strings.Contains(w.Body.String(), `"error"`) {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("bad error body %q: %v", w.Body.String(), err)
		}
	}
	return w, body
}

func TestRequestID_EchoedAndPropagated(t *testing.T) {
	r := newRouter(t)

	w, _ := do(t, r, "GET", "/ok", map[string]string{requestid.Header: "req-42"})
	if w.Header().Get(requestid.Header) != "req-42" {
		t.Errorf("response header = %q, want req-42", w.Header().Get(requestid.Header))
	}
	if !strings.Contains(w.Body.String(), "req-42") {
		t.Errorf("handler did not see the request id in context: %s", w.Body.String())
	}

	w, _ = do(t, r, "GET", "/ok", nil)
	if w.Header().Get(requestid.Header) == "" {
		t.Error("request id should be generated when absent")
	}
}

func TestDomainError_DefaultsTo400(t *testing.T) {
	w, body := do(t, newRouter(t), "GET", "/domain-error", nil)
	if w.Code != http.StatusBadRequest || body.Error.Code != "coupon_exhausted" {
		t.Fatalf("got %d %+v", w.Code, body)
	}
	if body.Error.RequestID == "" {
		t.Error("error body should carry the request id")
	}
}

func TestDomainError_WithErrorStatusMapping(t *testing.T) {
	w, body := do(t, newRouter(t), "GET", "/mapped-error", nil)
	if w.Code != http.StatusNotFound || body.Error.Code != "coupon_not_found" {
		t.Fatalf("got %d %+v", w.Code, body)
	}
}

func TestHTTPError_ExplicitStatus(t *testing.T) {
	w, body := do(t, newRouter(t), "GET", "/http-error", nil)
	if w.Code != http.StatusBadRequest || body.Error.Code != "invalid_request" {
		t.Fatalf("got %d %+v", w.Code, body)
	}
}

func TestInternalError_Masked(t *testing.T) {
	w, body := do(t, newRouter(t), "GET", "/internal", nil)
	if w.Code != http.StatusInternalServerError || body.Error.Code != "internal_error" {
		t.Fatalf("got %d %+v", w.Code, body)
	}
	if strings.Contains(w.Body.String(), "10.0.0.5") {
		t.Error("internal details leaked to the client")
	}
}

func TestPanic_Becomes500(t *testing.T) {
	w, body := do(t, newRouter(t), "GET", "/panic", nil)
	if w.Code != http.StatusInternalServerError || body.Error.Code != "internal_error" {
		t.Fatalf("got %d %+v", w.Code, body)
	}
}

// The auth pattern from the middleware guide, end to end: middleware verifies
// the token, puts a Principal into the request context, the handler reads it.
func TestAuthMiddleware_Pattern(t *testing.T) {
	r := httpapi.NewRouter(httpapi.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))

	authMiddleware := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer valid-token" {
			httpapi.Fail(c, httpapi.Unauthorized("unauthenticated"))
			return
		}
		ctx := auth.WithPrincipal(c.Request.Context(), auth.Principal{UserID: "u-1", Roles: []string{"admin"}})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}

	authed := r.Group("/api", authMiddleware)
	authed.GET("/me", func(c *gin.Context) {
		p, ok := auth.PrincipalFrom(c.Request.Context())
		if !ok {
			httpapi.Fail(c, errors.New("principal must be set by middleware"))
			return
		}
		c.JSON(http.StatusOK, gin.H{"userId": p.UserID, "admin": p.HasRole("admin")})
	})

	w, body := do(t, r, "GET", "/api/me", nil)
	if w.Code != http.StatusUnauthorized || body.Error.Code != "unauthenticated" {
		t.Fatalf("without token: got %d %+v", w.Code, body)
	}

	w, _ = do(t, r, "GET", "/api/me", map[string]string{"Authorization": "Bearer valid-token"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"userId":"u-1"`) {
		t.Fatalf("with token: got %d %s", w.Code, w.Body.String())
	}
}
