package runtime

import (
	"context"

	"github.com/docker/cagent/pkg/session"
)

// SequentialAgentOrchestrator runs agents one after another.
type SequentialAgentOrchestrator struct {
	runtimes []Runtime
}

func NewSequentialAgentOrchestrator(rts ...Runtime) *SequentialAgentOrchestrator {
	return &SequentialAgentOrchestrator{runtimes: rts}
}

func (o *SequentialAgentOrchestrator) Run(
	ctx context.Context,
	sess *session.Session,
) <-chan Event {
	out := make(chan Event)

	go func() {
		defer close(out)

		for _, rt := range o.runtimes {
			events := rt.RunStream(ctx, sess)
			for event := range events {
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return out
}
