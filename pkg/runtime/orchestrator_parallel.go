package runtime

import (
	"context"
	"sync"

	"github.com/docker/cagent/pkg/session"
)

// ParallelAgentOrchestrator runs agents in parallel but emits events
// in the order of the runtimes.
type ParallelAgentOrchestrator struct {
	runtimes []Runtime
}

func NewParallelAgentOrchestrator(rts ...Runtime) *ParallelAgentOrchestrator {
	return &ParallelAgentOrchestrator{runtimes: rts}
}

func (o *ParallelAgentOrchestrator) Run(
	ctx context.Context,
	sess *session.Session,
) <-chan Event {
	out := make(chan Event)

	type stream struct {
		events <-chan Event
	}

	streams := make([]stream, len(o.runtimes))

	var wg sync.WaitGroup
	wg.Add(len(o.runtimes))

	for i, rt := range o.runtimes {
		ch := make(chan Event)
		streams[i] = stream{events: ch}

		go func(rt Runtime, out chan Event) {
			defer wg.Done()
			defer close(out)

			events := rt.RunStream(ctx, sess)
			for event := range events {
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}(rt, ch)
	}

	go func() {
		defer close(out)

		// Emit strictly in runtime order
		for _, s := range streams {
			for event := range s.events {
				select {
				case out <- event:
				case <-ctx.Done():
					return
				}
			}
		}

		wg.Wait()
	}()

	return out
}
