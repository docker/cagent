package runtime

import (
	"context"

	"github.com/docker/cagent/pkg/session"
)

// SingleAgentOrchestrator runs exactly one agent (current behavior)
type SingleAgentOrchestrator struct {
	rt Runtime
}

func NewSingleAgentOrchestrator(rt Runtime) *SingleAgentOrchestrator {
	return &SingleAgentOrchestrator{rt: rt}
}

func (o *SingleAgentOrchestrator) Run(
	ctx context.Context,
	sess *session.Session,
) <-chan Event {
	return o.rt.RunStream(ctx, sess)
}
