package evaluation

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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

			judge := NewJudge(nil, nil, tt.concurrency)
			assert.Equal(t, tt.expectedConcurrency, judge.concurrency)
		})
	}
}

func TestJudge_CheckRelevance_EmptyCriteria(t *testing.T) {
	t.Parallel()

	judge := NewJudge(nil, nil, 1)
	passed, failed, errs := judge.CheckRelevance(t.Context(), "some response", nil)

	assert.Equal(t, 0, passed)
	assert.Empty(t, failed)
	assert.Empty(t, errs)
}

func TestJudge_CheckRelevance_ContextCanceled(t *testing.T) {
	t.Parallel()

	judge := NewJudge(nil, nil, 2)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	criteria := []string{"criterion1", "criterion2", "criterion3"}
	passed, failed, errs := judge.CheckRelevance(ctx, "some response", criteria)

	// All should have errors due to context cancellation
	assert.Equal(t, 0, passed)
	assert.Empty(t, failed)
	assert.Len(t, errs, 3)
	for _, err := range errs {
		assert.Contains(t, err, "context cancelled")
	}
}
