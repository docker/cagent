package think

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/docker-agent/pkg/tools"
)

func TestThinkTool_Handler(t *testing.T) {
	t.Parallel()
	tool := New()

	result, err := tool.callTool(t.Context(), Args{Thought: "This is a test thought"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "This is a test thought")

	result, err = tool.callTool(t.Context(), Args{Thought: "Another thought"})
	require.NoError(t, err)

	assert.Contains(t, result.Output, "This is a test thought")
	assert.Contains(t, result.Output, "Another thought")
}

func thinkHandler(t *testing.T) tools.ToolHandler {
	t.Helper()
	allTools, err := New().Tools(t.Context())
	require.NoError(t, err)
	require.Len(t, allTools, 1)
	return allTools[0].Handler
}

func callThink(ctx context.Context, handler tools.ToolHandler, thought string) (string, error) {
	result, err := handler(ctx, tools.ToolCall{
		Function: tools.FunctionCall{Name: ToolNameThink, Arguments: fmt.Sprintf(`{"thought":%q}`, thought)},
	}, tools.NopRuntime{})
	if err != nil {
		return "", err
	}
	return result.Output, nil
}

func TestThinkTool_SequentialOutput(t *testing.T) {
	t.Parallel()
	handler := thinkHandler(t)

	output, err := callThink(t.Context(), handler, "first")
	require.NoError(t, err)
	assert.Equal(t, "Thoughts:\nfirst", output)

	output, err = callThink(t.Context(), handler, "second")
	require.NoError(t, err)
	assert.Equal(t, "Thoughts:\nfirst\nsecond", output)
}

func TestThinkTool_ConcurrentCalls(t *testing.T) {
	t.Parallel()
	handler := thinkHandler(t)

	const n = 64
	thoughts := make([]string, n)
	outputs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		thoughts[i] = fmt.Sprintf("thought-%02d", i)
		wg.Go(func() {
			outputs[i], errs[i] = callThink(t.Context(), handler, thoughts[i])
		})
	}
	wg.Wait()

	for i := range n {
		require.NoError(t, errs[i])
		lines := strings.Split(outputs[i], "\n")
		assert.Equal(t, "Thoughts:", lines[0])
		assert.Contains(t, lines[1:], thoughts[i])
	}

	output, err := callThink(t.Context(), handler, "final")
	require.NoError(t, err)
	lines := strings.Split(output, "\n")
	require.Len(t, lines, n+2)
	assert.Equal(t, "Thoughts:", lines[0])
	assert.ElementsMatch(t, thoughts, lines[1:n+1])
	assert.Equal(t, "final", lines[n+1])
}

func TestThinkTool_OutputSchema(t *testing.T) {
	t.Parallel()
	tool := New()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestThinkTool_ParametersAreObjects(t *testing.T) {
	t.Parallel()
	tool := New()

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)

		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}
