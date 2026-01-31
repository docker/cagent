package memory

import (
	"context"
	"sync"

	"github.com/docker/cagent/pkg/config/latest"
)

var (
	globalRegistry     *Registry
	globalRegistryOnce sync.Once
)

// DefaultRegistry returns the global driver registry
func DefaultRegistry() *Registry {
	globalRegistryOnce.Do(func() {
		globalRegistry = NewRegistry()
	})
	return globalRegistry
}

// RegisterFactory registers a driver factory for a backend kind
func RegisterFactory(kind string, factory Factory) {
	DefaultRegistry().Register(kind, factory)
}

// CreateDriver creates a driver from config using the default registry
func CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (Driver, error) {
	return DefaultRegistry().CreateDriver(ctx, cfg)
}
