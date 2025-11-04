package auth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// Middleware creates an Echo middleware for JWT authentication
func Middleware(authManager *Manager, skipAuth bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Skip authentication if configured (for backward compatibility)
			if skipAuth {
				return next(c)
			}

			// Extract token from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
			}

			// Check for Bearer prefix
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid authorization header format")
			}

			tokenString := parts[1]

			// Validate token
			claims, err := authManager.ValidateToken(tokenString)
			if err != nil {
				if err == ErrTokenExpired {
					return echo.NewHTTPError(http.StatusUnauthorized, "token expired")
				}
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			// Get user from store
			user, err := authManager.GetUser(c.Request().Context(), claims.UserID)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "user not found")
			}

			// Add user to context
			ctx := ContextWithUser(c.Request().Context(), user)
			c.SetRequest(c.Request().WithContext(ctx))

			// Store user in Echo context for easy access
			c.Set("user", user)

			return next(c)
		}
	}
}

// OptionalAuthMiddleware checks for auth but doesn't require it
func OptionalAuthMiddleware(authManager *Manager) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract token from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader != "" {
				// Check for Bearer prefix
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && parts[0] == "Bearer" {
					tokenString := parts[1]

					// Validate token
					claims, err := authManager.ValidateToken(tokenString)
					if err == nil {
						// Get user from store
						user, err := authManager.GetUser(c.Request().Context(), claims.UserID)
						if err == nil {
							// Add user to context
							ctx := ContextWithUser(c.Request().Context(), user)
							c.SetRequest(c.Request().WithContext(ctx))
							c.Set("user", user)
						}
					}
				}
			}

			return next(c)
		}
	}
}

// AdminOnlyMiddleware ensures the user is an admin
func AdminOnlyMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		user, ok := c.Get("user").(*User)
		if !ok || user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
		}

		if !user.IsAdmin {
			return echo.NewHTTPError(http.StatusForbidden, "admin access required")
		}

		return next(c)
	}
}