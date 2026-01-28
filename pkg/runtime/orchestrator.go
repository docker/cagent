package runtime

import (
	"context"

	"github.com/docker/cagent/pkg/session"
)

// Orchestrator defines how agents are executed.
type Orchestrator interface {
	Run(ctx context.Context, sess *session.Session) <-chan Event
}
