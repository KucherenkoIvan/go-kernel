package webui

import (
	"io/fs"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Static serves embedded assets (vendored htmx, css, images) under
// relativePath with cache headers:
//
//	//go:embed static
//	var staticFS embed.FS
//
//	sub, _ := fs.Sub(staticFS, "static")
//	webui.Static(r, "/static", sub)
func Static(r gin.IRouter, relativePath string, fsys fs.FS) {
	group := r.Group(relativePath, func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=3600")
		c.Next()
	})
	group.StaticFS("/", http.FS(fsys))
}
