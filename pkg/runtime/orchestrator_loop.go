package runtime

import (
	"context"

	"github.com/docker/cagent/pkg/session"
)

// ExitCondition decides whether the loop should stop.
type ExitCondition func(
	ctx context.Context,
	iteration int,
	events []Event,
) bool

// LoopAgentOrchestrator runs an orchestrator repeatedly until an exit condition is met.
type LoopAgentOrchestrator struct {
	body           Orchestrator
	maxIterations  int
	exitConditions []ExitCondition
}

// NewLoopAgentOrchestrator creates a loop orchestrator.
// maxIterations <= 0 means unlimited.
func NewLoopAgentOrchestrator(
	body Orchestrator,
	maxIterations int,
	exitConditions ...ExitCondition,
) *LoopAgentOrchestrator {
	return &LoopAgentOrchestrator{
		body:           body,
		maxIterations:  maxIterations,
		exitConditions: exitConditions,
	}
}

func (o *LoopAgentOrchestrator) Run(
	ctx context.Context,
	sess *session.Session,
) <-chan Event {
	out := make(chan Event)

	go func() {
		defer close(out)

		iteration := 0

		for {
			if ctx.Err() != nil {
				return
			}

			if o.maxIterations > 0 && iteration >= o.maxIterations {
				return
			}

			events := o.body.Run(ctx, sess)

			var collected []Event

			for ev := range events {
				collected = append(collected, ev)

				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}

			for _, cond := range o.exitConditions {
				if cond(ctx, iteration, collected) {
					return
				}
			}

			iteration++
		}
	}()

	return out
}
