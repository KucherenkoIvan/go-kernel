// Package webui is the HTML presentation kit for Gin — server-rendered
// pages and htmx fragments over embedded templates, with error pages that
// coexist with httpapi's JSON contract (HTML routes render pages, /api keeps
// JSON; both flow through httpapi.Fail).
//
// Template convention (all under one embed.FS):
//
//	layouts/base.html   # the shell: defines "base", renders {{block "content" .}}
//	partials/*.html     # shared fragments (also served standalone to htmx)
//	pages/*.html        # one page per file; defines "content" (+ optional blocks)
//
// Presentation is an adapter: templates render read-models, never aggregates.
package webui

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/KucherenkoIvan/go-kernel/httpapi"
)

// Renderer holds the parsed template sets: one per page (layouts + partials
// + the page), plus a shared set for standalone fragments.
type Renderer struct {
	pages     map[string]*template.Template
	fragments *template.Template
}

type rendererConfig struct {
	funcs template.FuncMap
}

// Option customizes NewRenderer.
type Option func(*rendererConfig)

// WithFuncs adds template functions available to all pages and fragments.
func WithFuncs(funcs template.FuncMap) Option {
	return func(c *rendererConfig) { c.funcs = funcs }
}

// NewRenderer parses templates from fsys (typically an embed.FS, via fs.Sub
// when the files live under a subdirectory).
func NewRenderer(fsys fs.FS, opts ...Option) (*Renderer, error) {
	cfg := &rendererConfig{funcs: template.FuncMap{}}
	for _, opt := range opts {
		opt(cfg)
	}

	layouts, _ := fs.Glob(fsys, "layouts/*.html")
	partials, _ := fs.Glob(fsys, "partials/*.html")
	pageFiles, _ := fs.Glob(fsys, "pages/*.html")
	if len(pageFiles) == 0 {
		return nil, fmt.Errorf("webui: no pages/*.html found — wrong fs.Sub path?")
	}

	r := &Renderer{pages: make(map[string]*template.Template, len(pageFiles))}

	for _, pageFile := range pageFiles {
		name := strings.TrimSuffix(path.Base(pageFile), ".html")
		files := append(append(append([]string{}, layouts...), partials...), pageFile)
		t, err := template.New(name).Funcs(cfg.funcs).ParseFS(fsys, files...)
		if err != nil {
			return nil, fmt.Errorf("webui: parsing page %s: %w", pageFile, err)
		}
		r.pages[name] = t
	}

	if len(partials) > 0 {
		t, err := template.New("fragments").Funcs(cfg.funcs).ParseFS(fsys, partials...)
		if err != nil {
			return nil, fmt.Errorf("webui: parsing partials: %w", err)
		}
		r.fragments = t
	}
	return r, nil
}

// Render writes a full page: the "base" layout template when layouts exist,
// the page's own template otherwise. Output is buffered — a template error
// becomes a proper error response, never a half-written page.
func (r *Renderer) Render(c *gin.Context, status int, page string, data any) {
	t, ok := r.pages[page]
	if !ok {
		httpapi.Fail(c, fmt.Errorf("webui: unknown page %q", page))
		return
	}
	root := page
	if t.Lookup("base") != nil {
		root = "base"
	}
	r.execute(c, t, root, status, data)
}

// RenderFragment writes a single named partial — the htmx response shape.
func (r *Renderer) RenderFragment(c *gin.Context, status int, fragment string, data any) {
	if r.fragments == nil || r.fragments.Lookup(fragment) == nil {
		httpapi.Fail(c, fmt.Errorf("webui: unknown fragment %q", fragment))
		return
	}
	r.execute(c, r.fragments, fragment, status, data)
}

func (r *Renderer) execute(c *gin.Context, t *template.Template, name string, status int, data any) {
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		httpapi.Fail(c, fmt.Errorf("webui: rendering %s: %w", name, err))
		return
	}
	c.Data(status, "text/html; charset=utf-8", buf.Bytes())
}
