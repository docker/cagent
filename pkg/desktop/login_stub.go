//go:build no_docker_desktop

package desktop

import (
	"context"
	"os"
)

type DockerHubInfo struct {
	Email         string   `json:"email,omitempty"`
	Organizations []string `json:"organizations,omitempty"`
	PlanName      string   `json:"planName"`
}

// GetToken returns empty string when Docker Desktop is not available
func GetToken(ctx context.Context) string {
	// Allow the user to override the token via an environment variable.
	// This is e.g. useful when talking to a gateway on staging.
	manualToken := os.Getenv("DOCKER_TOKEN")
	if manualToken != "" {
		return manualToken
	}

	// Return empty string when Docker Desktop is not available
	return ""
}