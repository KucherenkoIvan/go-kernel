package webui_test

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"github.com/KucherenkoIvan/go-kernel/ddd"
	"github.com/KucherenkoIvan/go-kernel/httpapi"
	"github.com/KucherenkoIvan/go-kernel/webui"
)

type couponExhaustedError struct{ ddd.DomainError }

func (e *couponExhaustedError) Error() string { return "coupon_exhausted" }

var templates = fstest.MapFS{
	"layouts/base.html": &fstest.MapFile{Data: []byte(
		`{{define "base"}}<html><title>{{block "title" .}}app{{end}}</title><body>{{block "content" .}}{{end}}</body></html>{{end}}`)},
	"partials/row.html": &fstest.MapFile{Data: []byte(
		`{{define "row"}}<li>{{.}}</li>{{end}}`)},
	"pages/items.html": &fstest.MapFile{Data: []byte(
		`{{define "title"}}items{{end}}{{define "content"}}<ul>{{range .Items}}{{template "row" .}}{{end}}</ul>{{end}}`)},
	"pages/error.html": &fstest.MapFile{Data: []byte(
		`{{define "content"}}<h1>{{.Status}}</h1><p>{{.Code}}</p><small>{{.RequestID}}</small>{{end}}`)},
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func setup(t *testing.T) (*gin.Engine, *webui.Renderer) {
	t.Helper()
	renderer, err := webui.NewRenderer(templates)
	if err != nil {
		t.Fatal(err)
	}
	r := httpapi.NewRouter(httpapi.WithLogger(discard()))
	return r, renderer
}

func do(r *gin.Engine, method, path string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRender_PageThroughLayout(t *testing.T) {
	r, renderer := setup(t)
	r.GET("/items", func(c *gin.Context) {
		renderer.Render(c, http.StatusOK, "items", gin.H{"Items": []string{"a", "b"}})
	})

	w := do(r, "GET", "/items", nil)
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "<title>items</title>") ||
		!strings.Contains(body, "<li>a</li><li>b</li>") {
		t.Fatalf("got %d %q", w.Code, body)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type %q", ct)
	}
}

func TestRenderFragment(t *testing.T) {
	r, renderer := setup(t)
	r.GET("/row", func(c *gin.Context) {
		renderer.RenderFragment(c, http.StatusOK, "row", "x")
	})

	w := do(r, "GET", "/row", nil)
	if w.Code != 200 || w.Body.String() != "<li>x</li>" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

func TestRender_UnknownPageBecomesError(t *testing.T) {
	r, renderer := setup(t)
	r.GET("/nope", func(c *gin.Context) { renderer.Render(c, 200, "missing", nil) })

	w := do(r, "GET", "/nope", nil)
	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "internal_error") {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
}

// The coexistence contract: HTML group renders error pages, /api keeps JSON.
func TestErrorPages_CoexistWithJSON(t *testing.T) {
	r, renderer := setup(t)

	web := r.Group("/", webui.ErrorPages(renderer, "error",
		webui.WithLogger(discard()),
		webui.WithErrorStatus("coupon_exhausted", http.StatusConflict),
	))
	web.GET("/page-domain", func(c *gin.Context) { httpapi.Fail(c, &couponExhaustedError{}) })
	web.GET("/page-internal", func(c *gin.Context) { httpapi.Fail(c, errors.New("db exploded")) })
	web.GET("/page-http", func(c *gin.Context) { httpapi.Fail(c, httpapi.NotFound("no_such_page")) })

	r.GET("/api/domain", func(c *gin.Context) { httpapi.Fail(c, &couponExhaustedError{}) })

	// HTML group: domain error → mapped status, rendered page with code + request id
	w := do(r, "GET", "/page-domain", nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "<p>coupon_exhausted</p>") ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("domain page: %d %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "<small>") {
		t.Fatalf("request id missing: %q", w.Body.String())
	}

	// internals masked in HTML too
	w = do(r, "GET", "/page-internal", nil)
	if w.Code != 500 || !strings.Contains(w.Body.String(), "internal_error") ||
		strings.Contains(w.Body.String(), "db exploded") {
		t.Fatalf("internal page: %d %q", w.Code, w.Body.String())
	}

	// transport-level HTTPError keeps its explicit status
	if w = do(r, "GET", "/page-http", nil); w.Code != http.StatusNotFound {
		t.Fatalf("http error page: %d", w.Code)
	}

	// /api unaffected: same error, JSON contract
	w = do(r, "GET", "/api/domain", nil)
	if !strings.Contains(w.Body.String(), `"code":"coupon_exhausted"`) ||
		!strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("api: %d %q", w.Code, w.Body.String())
	}
}

func TestStatic(t *testing.T) {
	r, _ := setup(t)
	assets := fstest.MapFS{"htmx.min.js": &fstest.MapFile{Data: []byte("/*htmx*/")}}
	webui.Static(r, "/static", assets)

	w := do(r, "GET", "/static/htmx.min.js", nil)
	if w.Code != 200 || w.Body.String() != "/*htmx*/" || w.Header().Get("Cache-Control") == "" {
		t.Fatalf("got %d %q cache=%q", w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
	}
}

func TestHTMXHelpers(t *testing.T) {
	r, _ := setup(t)
	r.GET("/go", func(c *gin.Context) { webui.Redirect(c, "/there") })

	// plain request → 303
	w := do(r, "GET", "/go", nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/there" {
		t.Fatalf("plain: %d %q", w.Code, w.Header().Get("Location"))
	}

	// htmx request → HX-Redirect header
	w = do(r, "GET", "/go", map[string]string{"HX-Request": "true"})
	if w.Code != http.StatusNoContent || w.Header().Get("HX-Redirect") != "/there" {
		t.Fatalf("htmx: %d %q", w.Code, w.Header().Get("HX-Redirect"))
	}
}
