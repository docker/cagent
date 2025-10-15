package codemode

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunJavascript(t *testing.T) {
	tool := &codeModeTool{}

	result, err := tool.runJavascript(t.Context(), `return "HELLO"`)
	require.NoError(t, err)

	assert.Equal(t, "HELLO", result.Value)
	assert.Empty(t, result.StdOut)
	assert.Empty(t, result.StdErr)
}

func TestRunJavascript_error(t *testing.T) {
	tool := &codeModeTool{}

	result, err := tool.runJavascript(t.Context(), `==`)
	require.NoError(t, err)

	assert.Equal(t, "SyntaxError: SyntaxError: (anonymous): Line 2:1 Unexpected token == (and 2 more errors)", result.Value)
	assert.Empty(t, result.StdOut)
	assert.Empty(t, result.StdErr)
}

func TestRunJavascript_console(t *testing.T) {
	tool := &codeModeTool{}

	result, err := tool.runJavascript(t.Context(), `console.log("to stdout"); console.error("to stderr"); return "RESULT";`)
	require.NoError(t, err)

	assert.Equal(t, "RESULT", result.Value)
	assert.Equal(t, "to stdout\n", result.StdOut)
	assert.Equal(t, "to stderr\n", result.StdErr)
}

func TestRunJavascript_no_result(t *testing.T) {
	tool := &codeModeTool{}

	result, err := tool.runJavascript(t.Context(), ``)
	require.NoError(t, err)

	assert.Empty(t, result.Value)
	assert.Empty(t, result.StdOut)
	assert.Empty(t, result.StdErr)
}
