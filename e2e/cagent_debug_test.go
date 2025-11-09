package e2e_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/cmd/root"
)

func TestDebug_Toolsets_None(t *testing.T) {
	t.Parallel()

	output := cagentDebug(t, "toolsets", "testdata/no_tools.yaml")

	require.Equal(t, "No tools for root\n", output)
}

func TestDebug_Toolsets_Todo(t *testing.T) {
	t.Parallel()

	output := cagentDebug(t, "toolsets", "testdata/todo_tools.yaml")

	require.Equal(t, "2 tool(s) for root:\n + create_todo\n + list_todos\n", output)
}

func cagentDebug(t *testing.T, moreArgs ...string) string {
	t.Helper()

	// `cagent debug ...`
	args := []string{"debug"}

	// Use .env file to set DUMMY OPENAI key
	dotEnv := filepath.Join(t.TempDir(), ".env")
	err := os.WriteFile(dotEnv, []byte("OPENAI_API_KEY=DUMMY"), 0o644)
	require.NoError(t, err)
	args = append(args, "--env-from-file", dotEnv)

	// Run cagent debug
	var stdout bytes.Buffer
	err = root.Execute(t.Context(), nil, &stdout, io.Discard, append(args, moreArgs...)...)
	require.NoError(t, err)

	return stdout.String()
}
