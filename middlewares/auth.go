package middlewares

import (
	"gostream/modules"
	"net/http"

	"github.com/labstack/echo/v4"
)

// BasicAuth middleware checks HTTP Basic Authentication
func BasicAuth(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		// Skip auth if credentials are not configured
		if modules.Config.Username == "" || modules.Config.Password == "" {
			return next(ctx)
		}

		username, password, ok := ctx.Request().BasicAuth()
		
		// Check if credentials match
		if !ok || username != modules.Config.Username || password != modules.Config.Password {
			ctx.Response().Header().Set("WWW-Authenticate", "Basic realm=\"GoStream\"")
			return ctx.JSON(http.StatusUnauthorized, map[string]string{
				"error": "Unauthorized",
				"message": "Invalid username or password",
			})
		}

		return next(ctx)
	}
}

