package sessiontitle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerator_GenerateEmptyMessages(t *testing.T) {
	t.Parallel()

	// Create a generator with nil model (won't be used since messages are empty)
	gen := New(nil)

	// Call Generate with empty user messages - should return early without doing anything
	title, err := gen.Generate(t.Context(), "test-session", []string{})
	require.NoError(t, err)
	assert.Empty(t, title)

	// Also test with nil slice
	title, err = gen.Generate(t.Context(), "test-session", nil)
	require.NoError(t, err)
	assert.Empty(t, title)
}
