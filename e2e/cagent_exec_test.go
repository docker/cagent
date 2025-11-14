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

func TestExec_OpenAI(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n\n2 + 2 equals 4.", out)
}

func TestExec_OpenAI_ToolCall(t *testing.T) {
	t.Skip("flaky because of tool call ordering")

	out := cagentExec(t, "testdata/fs_tools.yaml", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n\nCalling search_files(\n  path: \"testdata/working_dir\"\n  pattern: \"*\"\n)\n\nsearch_files response → \"2 files found:\\ntestdata/working_dir\\ntestdata/working_dir/README.me\"\n\n2", out)
}

func TestExec_OpenAI_gpt5(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=openai/gpt-5", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n\n4", out)
}

func TestExec_Anthropic(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=anthropic/claude-sonnet-4-0", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n\n2 + 2 = 4", out)
}

func TestExec_Anthropic_ToolCall(t *testing.T) {
	t.Skip("flaky because of tool call ordering")

	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=anthropic/claude-sonnet-4-0", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n\nCalling list_directory(path: \"testdata/working_dir\")\n\nlist_directory response → \"FILE README.me\\n\"\n\n1", out)
}

func TestExec_Gemini(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=google/gemini-2.5-flash", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n\n2 + 2 = 4", out)
}

func TestExec_Gemini_ToolCall(t *testing.T) {
	t.Skip("flaky because of tool call ordering")

	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=google/gemini-2.5-flash", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n\nCalling list_directory(path: \"testdata/working_dir\")\n\nlist_directory response → \"FILE README.me\\n\"\n\n1", out)
}

func TestExec_Mistral(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=mistral/mistral-small", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n\nThe sum of 2 + 2 is 4.", out)
}

func TestExec_Mistral_ToolCall(t *testing.T) {
	t.Skip("This is awkard")

	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=mistral/mistral-small", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n", out)
}

func TestExec_ToolCallsNeedAcceptance(t *testing.T) {
	out := cagentExec(t, "testdata/file_writer.yaml", "Create a hello.txt file with \"Hello, World!\" content. Try only once. On error, exit without further message.")

	require.Contains(t, out, `Can I run this tool? ([y]es/[a]ll/[n]o)`)
}

func cagentExec(t *testing.T, moreArgs ...string) string {
	t.Helper()

	// `cagent exec ...`
	args := []string{"exec"}

	// Use a dummy .env file to avoid using real JWT. Our proxy server doesn't need it.
	dotEnv := filepath.Join(t.TempDir(), ".env")
	err := os.WriteFile(dotEnv, []byte("DOCKER_TOKEN=DUMMY"), 0o644)
	require.NoError(t, err)
	args = append(args, "--env-from-file", dotEnv)

	// Start a recording AI proxy to record and replay traffic.
	svr, _ := startRecordingAIProxy(t)
	args = append(args, "--models-gateway", svr.URL)

	// Run cagent exec
	var stdout bytes.Buffer
	err = root.Execute(t.Context(), nil, &stdout, io.Discard, append(args, moreArgs...)...)
	require.NoError(t, err)

	return stdout.String()
}
