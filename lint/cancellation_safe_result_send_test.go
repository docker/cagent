package main

import (
	"os"
	"testing"

	"github.com/dgageot/rubocop-go/coptest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancellationSafeResultSendReadLine(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../pkg/input/readline.go")
	require.NoError(t, err)
	offenses := coptest.RunProgram(t, registeredSharedCop(t, programCops, "Lint/CancellationSafeResultSend"), coptest.ProgramFiles{"input/readline.go": string(src)})
	assert.Empty(t, offenses)
}
