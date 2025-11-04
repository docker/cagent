package server

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/docker/cagent/pkg/auth"
)

// RegisterRequest represents the registration request
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

// LoginRequest represents the login request
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthResponse represents the authentication response
type AuthResponse struct {
	Token string     `json:"token"`
	User  *auth.User `json:"user"`
}

// register handles user registration
func (s *Server) register(c echo.Context) error {
	// Registration is disabled if auth is disabled
	if s.authDisabled {
		return echo.NewHTTPError(http.StatusMethodNotAllowed, "authentication is disabled")
	}

	if s.authManager == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "authentication not configured")
	}

	var req RegisterRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Manual validation
	if req.Email == "" || req.Password == "" || req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "email, password, and name are required")
	}
	if len(req.Password) < 8 {
		return echo.NewHTTPError(http.StatusBadRequest, "password must be at least 8 characters")
	}

	// Register the user
	user, err := s.authManager.RegisterUser(c.Request().Context(), req.Email, req.Password, req.Name)
	if err != nil {
		if err == auth.ErrUserExists {
			return echo.NewHTTPError(http.StatusConflict, "user already exists")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to register user")
	}

	// Generate token for the new user
	token, err := s.authManager.GenerateToken(user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate token")
	}

	return c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

// login handles user login
func (s *Server) login(c echo.Context) error {
	// Login is disabled if auth is disabled
	if s.authDisabled {
		return echo.NewHTTPError(http.StatusMethodNotAllowed, "authentication is disabled")
	}

	if s.authManager == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "authentication not configured")
	}

	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Manual validation
	if req.Email == "" || req.Password == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "email and password are required")
	}

	// Authenticate the user
	token, user, err := s.authManager.LoginUser(c.Request().Context(), req.Email, req.Password)
	if err != nil {
		if err == auth.ErrInvalidCredentials {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to login")
	}

	return c.JSON(http.StatusOK, AuthResponse{
		Token: token,
		User:  user,
	})
}

// getCurrentUser returns the current authenticated user
func (s *Server) getCurrentUser(c echo.Context) error {
	user, ok := c.Get("user").(*auth.User)
	if !ok || user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	return c.JSON(http.StatusOK, user)
}