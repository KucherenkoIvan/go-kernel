package webui

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// IsHTMX reports whether the request came from htmx — the fragment-vs-full-
// page decision:
//
//	if webui.IsHTMX(c) {
//		renderer.RenderFragment(c, http.StatusOK, "item_row", model)
//		return
//	}
//	renderer.Render(c, http.StatusOK, "items", data)
func IsHTMX(c *gin.Context) bool {
	return c.GetHeader("HX-Request") == "true"
}

// Redirect sends the client to location the way it can actually follow:
// htmx requests get an HX-Redirect header (a 3xx would be swallowed by the
// fragment swap), plain requests get a 303 See Other.
func Redirect(c *gin.Context, location string) {
	if IsHTMX(c) {
		c.Header("HX-Redirect", location)
		c.Status(http.StatusNoContent)
		return
	}
	c.Redirect(http.StatusSeeOther, location)
}
