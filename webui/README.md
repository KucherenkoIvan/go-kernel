# webui

The HTML presentation kit for Gin: server-rendered pages and htmx fragments over embedded templates, coexisting with `httpapi`'s JSON contract. For the Go+htmx single-binary web stack — no node toolchain.

- `NewRenderer(fs)` — parses `layouts/` + `partials/` + `pages/` from an embed.FS; `Render` (full page through the layout, buffered) and `RenderFragment` (a named partial — the htmx response shape)
- `ErrorPages(renderer, page, ...)` — group-scoped middleware rendering failures as HTML pages with the same precedence as the JSON side (`HTTPError` → its status, domain errors → 400 or `WithErrorStatus`, everything else masked as 500); `/api` keeps JSON untouched
- `Static(r, path, fs)` — embedded assets (vendored htmx, css) with cache headers
- `IsHTMX(c)` / `Redirect(c, location)` — fragment-vs-page decisions and redirects htmx can actually follow

```go
renderer, _ := webui.NewRenderer(templatesFS)
webui.Static(r, "/static", assetsFS)

web := r.Group("/", webui.ErrorPages(renderer, "error"))
web.GET("/items", func(c *gin.Context) {
	items, err := listQuery.Execute(c.Request.Context())
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	renderer.Render(c, http.StatusOK, "items", gin.H{"Items": items})
})
```

Presentation is an adapter: templates render **read-models, never aggregates**.

Guide: [docs/guides/webui.md](../docs/guides/webui.md)
