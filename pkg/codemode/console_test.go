package codemode

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsole_debug(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["debug"]("debug message")

	assert.Equal(t, "debug message\n", stdOut.String())
	assert.Empty(t, stdErr.String())
}

func TestConsole_info(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["info"]("info message")

	assert.Equal(t, "info message\n", stdOut.String())
	assert.Empty(t, stdErr.String())
}

func TestConsole_log(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["log"]("log message")

	assert.Equal(t, "log message\n", stdOut.String())
	assert.Empty(t, stdErr.String())
}

func TestConsole_trace(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["trace"]("trace message")

	assert.Equal(t, "trace message\n", stdOut.String())
	assert.Empty(t, stdErr.String())
}

func TestConsole_warn(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["warn"]("warn message")

	assert.Equal(t, "warn message\n", stdOut.String())
	assert.Empty(t, stdErr.String())
}

func TestConsole_error(t *testing.T) {
	t.Parallel()

	var stdOut bytes.Buffer
	var stdErr bytes.Buffer
	console(&stdOut, &stdErr)["error"]("error message")

	assert.Empty(t, stdOut.String())
	assert.Equal(t, "error message\n", stdErr.String())
}
