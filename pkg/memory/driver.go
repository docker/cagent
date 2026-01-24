package memory

import (
	"context"
	"io"

	"github.com/docker/cagent/pkg/config/latest"
)

// Driver defines the interface for memory backends.
// Implementations support different strategies (long-term RAG, short-term whiteboard).
type Driver interface {
	// Store saves a memory entry with the given key and value
	Store(ctx context.Context, key, value string) error

	// Retrieve fetches memory entries matching the query
	Retrieve(ctx context.Context, query Query) ([]Entry, error)

	// Delete removes a memory entry by key
	Delete(ctx context.Context, key string) error

	// Close releases resources held by the driver
	io.Closer
}

// Query represents different types of memory queries
type Query struct {
	// ID for exact match retrieval
	ID string

	// Semantic for natural language queries (GraphRAG, vector search)
	Semantic string

	// Limit on number of results
	Limit int

	// Filters for metadata-based filtering
	Filters map[string]any
}

// Entry represents a memory item returned from a query
type Entry struct {
	ID        string
	CreatedAt string
	Content   string
	Metadata  map[string]any
	Score     float64 // Relevance score for semantic queries
}

// Factory creates memory drivers from configuration
type Factory interface {
	CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (Driver, error)
}

// Registry holds registered driver factories
type Registry struct {
	factories map[string]Factory
}

// NewRegistry creates a new driver registry
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register adds a factory for a specific backend kind
func (r *Registry) Register(kind string, factory Factory) {
	r.factories[kind] = factory
}

// CreateDriver instantiates a driver from config
func (r *Registry) CreateDriver(ctx context.Context, cfg latest.MemoryConfig) (Driver, error) {
	factory, ok := r.factories[cfg.Kind]
	if !ok {
		return nil, &UnsupportedKindError{Kind: cfg.Kind}
	}
	return factory.CreateDriver(ctx, cfg)
}

// UnsupportedKindError indicates an unknown backend kind
type UnsupportedKindError struct {
	Kind string
}

func (e *UnsupportedKindError) Error() string {
	return "unsupported memory kind: " + e.Kind
}
