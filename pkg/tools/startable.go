package tools

import (
	"context"
	"sync"
)

// StartableToolSet wraps a ToolSet with lazy, single-flight start semantics.
// This is the canonical way to manage toolset lifecycle.
type StartableToolSet struct {
	ToolSet

	mu      sync.Mutex
	started bool
}

// NewStartable wraps a ToolSet for lazy initialization.
func NewStartable(ts ToolSet) *StartableToolSet {
	return &StartableToolSet{ToolSet: ts}
}

// IsStarted returns whether the toolset has been successfully started.
// For toolsets that don't implement Startable, this always returns true.
func (s *StartableToolSet) IsStarted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// Start starts the toolset with single-flight semantics.
// Concurrent callers block until the start attempt completes.
// If start fails, a future call will retry.
// If the underlying toolset doesn't implement Startable, this is a no-op.
func (s *StartableToolSet) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil
	}

	if startable, ok := s.ToolSet.(Startable); ok {
		if err := startable.Start(ctx); err != nil {
			return err
		}
	}
	s.started = true
	return nil
}

// Stop stops the toolset if it implements Startable.
func (s *StartableToolSet) Stop(ctx context.Context) error {
	if startable, ok := s.ToolSet.(Startable); ok {
		return startable.Stop(ctx)
	}
	return nil
}

// Unwrap returns the underlying ToolSet.
func (s *StartableToolSet) Unwrap() ToolSet {
	return s.ToolSet
}

// As performs a type assertion on a ToolSet, unwrapping StartableToolSet if needed.
// Returns the typed toolset and true if the assertion succeeds.
//
// Example:
//
//	if pp, ok := tools.As[tools.PromptProvider](toolset); ok {
//	    prompts, _ := pp.ListPrompts(ctx)
//	}
func As[T any](ts ToolSet) (T, bool) {
	// Unwrap if it's a StartableToolSet
	if startable, ok := ts.(*StartableToolSet); ok {
		ts = startable.ToolSet
	}
	result, ok := ts.(T)
	return result, ok
}
