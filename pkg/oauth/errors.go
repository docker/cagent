package oauth

import (
	"fmt"
)

// AuthorizationRequiredError wraps an OAuth authorization error with server information
type AuthorizationRequiredError struct {
	Err        error
	ServerURL  string
	ServerType string
}

func (e *AuthorizationRequiredError) Error() string {
	return fmt.Sprintf("OAuth authorization required for %s server '%s': %v", e.ServerType, e.ServerURL, e.Err)
}

func (e *AuthorizationRequiredError) Unwrap() error {
	return e.Err
}
