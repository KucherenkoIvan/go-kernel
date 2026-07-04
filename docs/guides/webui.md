# Web UI (server-rendered HTML + htmx)

The single-binary web stack: Go templates rendered from read-models, htmx for interactivity — one static executable, no node toolchain. Package readme: [`webui/README.md`](../../webui/README.md).

## Where the web UI sits in the architecture

A server-rendered UI is a **presentation adapter** — `features/<feature>/adapters/web/`, the same altitude as `adapters/rest/`, calling the same queries and commands. The rules carry over verbatim:

- Templates render **read-models, never aggregates**.
- `gin.Context` and templates stay inside the adapter; use-cases see `context.Context` + typed arguments.
- Failures go through `httpapi.Fail` — the error *page* is middleware's job, like the JSON contract on `/api`.

## Template layout

One embedded directory, three kinds of files:

```
web/
  layouts/base.html    # the shell: defines "base", renders {{block "content" .}}
  partials/*.html      # shared fragments — also served standalone to htmx
  pages/*.html         # one page per file; defines "content" (+ optional "title" etc.)
static/
  htmx.min.js          # vendored — no CDN, the binary stays self-contained
  app.css
```

```go
//go:embed web
var webFS embed.FS
//go:embed static
var staticFS embed.FS

templates, _ := fs.Sub(webFS, "web")
renderer, err := webui.NewRenderer(templates) // webui.WithFuncs(...) for template funcs

assets, _ := fs.Sub(staticFS, "static")
webui.Static(r, "/static", assets)
```

Each page is parsed as its own template set (layouts + partials + the page), so every page can define `content` without collisions. `Render` executes through `base`; output is buffered — a template error becomes a proper error response, never a half-written page.

## Pages and fragments (the htmx loop)

Full page on navigation, fragment on interaction — one handler can serve both:

```go
web.GET("/items", func(c *gin.Context) {
	items, err := h.list.Execute(c.Request.Context())
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	if webui.IsHTMX(c) { // htmx refresh: just the fragment
		renderer.RenderFragment(c, http.StatusOK, "item_list", items)
		return
	}
	renderer.Render(c, http.StatusOK, "items", gin.H{"Items": items})
})

web.POST("/items", func(c *gin.Context) {
	id, err := h.create.Execute(c.Request.Context(), c.PostForm("name"))
	if err != nil {
		httpapi.Fail(c, err)
		return
	}
	_ = id
	webui.Redirect(c, "/items") // HX-Redirect for htmx, 303 otherwise
})
```

```html
<!-- pages/items.html -->
{{define "content"}}
<form hx-post="/items" hx-target="#list">
  <input name="name" required>
  <button>Add</button>
</form>
<div id="list">{{template "item_list" .Items}}</div>
{{end}}
```

## Error pages next to the JSON contract

`ErrorPages` is **group-scoped**: it renders HTML for the web routes and writes first, so `httpapi`'s global JSON middleware skips those requests — `/api` keeps its contract with zero configuration:

```go
web := r.Group("/", webui.ErrorPages(renderer, "error",
	webui.WithErrorStatus("changeme_not_found", http.StatusNotFound),
))
```

The error page template receives `webui.ErrorPageData{Status, Code, RequestID}` — same information as the JSON body, same masking guarantee (internals are logged, never rendered).

Known gap, accepted for now: a **panic** in a web handler is recovered by the global chain and produces a JSON 500, not an error page. Rare enough to live with; revisit if it ever matters.

## Live updates

Server push (htmx's SSE extension over domain events) is deliberately not built yet — it belongs to a future `realtime` package (hub over the `ChannelPublisher`, backpressure, shutdown ordering). Until then, htmx polling (`hx-trigger="every 5s"`) covers most tiny-app needs honestly.

## Anti-patterns

| Anti-pattern | The right way |
|-------------|---------------|
| Templates receiving aggregates | Read-models only — same rule as every transport |
| htmx from a CDN | Vendor the file into `static/` — the binary stays self-contained |
| Hand-rolled error HTML in handlers | `httpapi.Fail` + `ErrorPages` middleware |
| Business logic in template funcs | Funcs format; use-cases decide |
| One giant template set for all pages | Per-page sets (NewRenderer's convention) — blocks stay collision-free |
| Web routes outside the `ErrorPages` group | Users see JSON errors on HTML pages |
