package tool

import (
	"sync"

	"github.com/docker/cagent/pkg/tui/core/layout"
	"github.com/docker/cagent/pkg/tui/service"
	"github.com/docker/cagent/pkg/tui/types"
)

// ComponentBuilder is a function that creates a tool component.
type ComponentBuilder func(
	msg *types.Message,
	sessionState *service.SessionState,
) layout.Model

// Registry manages tool component builders.
type Registry struct {
	mu       sync.RWMutex
	builders map[string]ComponentBuilder
}

func NewRegistry() *Registry {
	return &Registry{
		builders: make(map[string]ComponentBuilder),
	}
}

func (r *Registry) Register(toolName string, builder ComponentBuilder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.builders[toolName] = builder
}

func (r *Registry) Get(toolName string) (ComponentBuilder, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	builder, exists := r.builders[toolName]
	return builder, exists
}
