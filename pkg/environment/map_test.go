package environment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapProvider_Get(t *testing.T) {
	provider := NewMapProvider(map[string]string{
		"API_KEY":    "secret123",
		"SESSION_ID": "sess-abc",
		"EMPTY":      "",
	})

	ctx := context.Background()

	// Test existing keys
	val, found := provider.Get(ctx, "API_KEY")
	assert.True(t, found)
	assert.Equal(t, "secret123", val)

	// Test empty value (but key exists)
	val, found = provider.Get(ctx, "EMPTY")
	assert.True(t, found)
	assert.Equal(t, "", val)

	// Test non-existent key
	val, found = provider.Get(ctx, "NOT_FOUND")
	assert.False(t, found)
	assert.Equal(t, "", val)
}

func TestMapProvider_MultiProvider_Priority(t *testing.T) {
	// Session overrides
	sessionProvider := NewMapProvider(map[string]string{
		"API_KEY": "session-override",
	})

	// System env
	osProvider := NewMapProvider(map[string]string{
		"API_KEY": "system-value",
		"OTHER":   "system-only",
	})

	// Chain: session first, os fallback
	multi := NewMultiProvider(sessionProvider, osProvider)
	ctx := context.Background()

	// Session override should win
	val, found := multi.Get(ctx, "API_KEY")
	assert.True(t, found)
	assert.Equal(t, "session-override", val)

	// Fallback to system
	val, found = multi.Get(ctx, "OTHER")
	assert.True(t, found)
	assert.Equal(t, "system-only", val)
}
