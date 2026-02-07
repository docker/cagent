// Package background provides concurrent agents feature support.
package background

import (
	"os"
)

const (
	// EnvConcurrentAgents is the environment variable to enable concurrent agents.
	EnvConcurrentAgents = "CAGENT_EXPERIMENTAL_CONCURRENT_AGENTS"
)

// IsEnabled returns true if the concurrent agents feature is enabled.
func IsEnabled() bool {
	return os.Getenv(EnvConcurrentAgents) == "1"
}
