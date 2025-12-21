package auth

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// OIDCAuthMiddleware returns a Gin middleware for OIDC authentication
func OIDCAuthMiddleware(auth *OIDCAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)

		// Check for existing valid session
		userEmail := session.Get(SessionKeyOIDCEmail)
		if userEmail != nil {
			c.Set("user", userEmail.(string))
			c.Next()
			return
		}

		// Not authenticated - allow auth routes without redirect
		path := c.Request.URL.Path
		if path == "/auth/login" || path == "/auth/callback" || path == "/auth/logout" {
			c.Next()
			return
		}

		// Allow static files without authentication
		if len(path) >= 7 && path[:7] == "/static" {
			c.Next()
			return
		}

		// Redirect to login
		c.Redirect(http.StatusFound, "/auth/login")
		c.Abort()
	}
}
