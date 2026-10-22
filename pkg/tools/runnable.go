package tools

import "context"

// toolsetRun bridges blocking workers to the host's Start/Stop lifecycle.
// Only the canonical wrapper owns cancellation and joining; workers keep
// their resources local to Run.
type toolsetRun struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

func startToolsetRun(ctx context.Context, runnable Runnable) *toolsetRun {
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		runnable.Run(ctx)
	}()
	return &toolsetRun{cancel: cancel, done: done}
}

func (r *toolsetRun) finished() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

func (r *toolsetRun) stop() {
	r.cancel()
	<-r.done
}

// isStartedLocked also accounts for workers that returned on their own.
func (s *StartableToolSet) isStartedLocked() bool {
	if !s.started || (s.run != nil && s.run.finished()) {
		return false
	}
	if owner, ok := As[*StartableToolSet](s.ToolSet); ok {
		return owner.TryIsStarted()
	}
	return true
}
