package workflow

import (
	"context"
	"fmt"
	"sync"
)

type loopCounterKey struct{}

type loopCounter struct {
	mu       sync.Mutex
	counts   map[string]int
	maxPerID int
}

// NewLoopCounter attaches a loop counter to ctx. maxIterations is the maximum
// number of times any single step ID can be executed (loop back-edges).
func NewLoopCounter(ctx context.Context, maxIterations int) context.Context {
	return context.WithValue(ctx, loopCounterKey{}, &loopCounter{
		counts:   make(map[string]int),
		maxPerID: maxIterations,
	})
}

// IncLoopCounter increments the execution count for stepID and returns an error
// if the count exceeds the configured maximum (prevents infinite loops).
func IncLoopCounter(ctx context.Context, stepID string) error {
	lc, ok := ctx.Value(loopCounterKey{}).(*loopCounter)
	if !ok {
		return nil
	}
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.counts[stepID]++
	if lc.counts[stepID] > lc.maxPerID {
		return fmt.Errorf("workflow: max loop iterations exceeded (step: %s, limit: %d)", stepID, lc.maxPerID)
	}
	return nil
}
