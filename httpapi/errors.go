package httpapi

// ErrorResponse is the uniform error body:
//
//	{"error": {"code": "coupon_exhausted", "requestId": "0190..."}}
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	RequestID string `json:"requestId,omitempty"`
}

// HTTPError is a transport-level error with an explicit status — for
// failures that are not domain errors but must not read as 500s: malformed
// input, missing auth, unknown routes.
type HTTPError struct {
	Status int
	Code   string
	cause  error
}

func (e *HTTPError) Error() string { return e.Code }
func (e *HTTPError) Unwrap() error { return e.cause }

// NewError builds an HTTPError with an explicit status.
func NewError(status int, code string, cause error) *HTTPError {
	return &HTTPError{Status: status, Code: code, cause: cause}
}

// BadRequest is a 400 — the standard wrapper for binding/validation errors:
//
//	if err := c.ShouldBindJSON(&req); err != nil {
//		httpapi.Fail(c, httpapi.BadRequest("invalid_request", err))
//		return
//	}
func BadRequest(code string, cause error) *HTTPError {
	return &HTTPError{Status: 400, Code: code, cause: cause}
}

// Unauthorized is a 401 — for authentication middleware.
func Unauthorized(code string) *HTTPError {
	return &HTTPError{Status: 401, Code: code}
}

// Forbidden is a 403 — for coarse authorization middleware.
func Forbidden(code string) *HTTPError {
	return &HTTPError{Status: 403, Code: code}
}

// NotFound is a 404.
func NotFound(code string) *HTTPError {
	return &HTTPError{Status: 404, Code: code}
}
