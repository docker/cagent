package evaluation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewJudge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                string
		concurrency         int
		expectedConcurrency int
	}{
		{
			name:                "concurrency 0 defaults to 1",
			concurrency:         0,
			expectedConcurrency: 1,
		},
		{
			name:                "custom concurrency",
			concurrency:         5,
			expectedConcurrency: 5,
		},
		{
			name:                "negative concurrency defaults to 1",
			concurrency:         -3,
			expectedConcurrency: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			judge := NewJudge(nil, tt.concurrency)
			assert.Equal(t, tt.expectedConcurrency, judge.concurrency)
		})
	}
}

func TestJudge_CheckRelevance_EmptyCriteria(t *testing.T) {
	t.Parallel()

	judge := NewJudge(nil, 1)
	passed, failed, err := judge.CheckRelevance(t.Context(), "some response", nil)

	assert.Equal(t, 0, passed)
	assert.Empty(t, failed)
	assert.NoError(t, err)
}

func TestJudge_CheckRelevance_ContextCanceled(t *testing.T) {
	t.Parallel()

	judge := NewJudge(nil, 2)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	criteria := []string{"criterion1", "criterion2", "criterion3"}
	passed, failed, err := judge.CheckRelevance(ctx, "some response", criteria)

	// All should have errors due to context cancellation
	assert.Equal(t, 0, passed)
	assert.Empty(t, failed)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context cancelled")
}
