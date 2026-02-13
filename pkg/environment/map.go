package environment

import "context"

// MapProvider provides environment variables from an in-memory map.
// Used for injecting session-specific values that take precedence over other providers.
type MapProvider struct {
	values map[string]string
}

// NewMapProvider creates a new MapProvider with the given key-value pairs.
// The map should not be modified after creation to ensure thread-safety.
func NewMapProvider(values map[string]string) *MapProvider {
	return &MapProvider{
		values: values,
	}
}

// Get retrieves a value from the map.
// Returns (value, true) if the key exists, ("", false) otherwise.
func (p *MapProvider) Get(_ context.Context, name string) (string, bool) {
	val, found := p.values[name]
	return val, found
}
