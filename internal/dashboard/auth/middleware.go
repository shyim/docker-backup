package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// BasicAuthMiddleware returns a Gin middleware that enforces HTTP Basic Authentication
// using htpasswd-style credentials
func BasicAuthMiddleware(auth *HtpasswdAuth) gin.HandlerFunc {
	return func(c *gin.Context) {
		username, password, hasAuth := c.Request.BasicAuth()

		if !hasAuth || !auth.Authenticate(username, password) {
			c.Header("WWW-Authenticate", `Basic realm="Docker Backup Dashboard"`)
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}

		c.Set("user", username)
		c.Next()
	}
}
