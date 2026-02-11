package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStepContext_EvalCondition(t *testing.T) {
	t.Parallel()
	ctx := NewStepContext()
	ctx.SetAgentOutput("qa", `{"is_approved": true}`, "qa_agent")

	ok, resolved := ctx.EvalCondition("{{ $steps.qa.output.is_approved }}")
	require.True(t, resolved)
	assert.True(t, ok)

	ctx.SetAgentOutput("qa", `{"is_approved": false}`, "qa_agent")
	ok, resolved = ctx.EvalCondition("{{ $steps.qa.output.is_approved }}")
	require.True(t, resolved)
	assert.False(t, ok)
}
