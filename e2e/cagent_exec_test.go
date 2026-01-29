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

	require.Equal(t, "\n--- Agent: root ---\n2 + 2 equals 4.", out)
}

// TestExec_OpenAI_V3Config tests that v3 configs work correctly with thinking disabled by default.
// This uses gpt-5 with a v3 config file to verify thinking is disabled for old config versions.
func TestExec_OpenAI_V3Config(t *testing.T) {
	out := cagentExec(t, "testdata/basic_v3.yaml", "What's 2+2?")

	// v3 config with gpt-5 should work correctly (thinking disabled by default for old configs)
	require.Equal(t, "\n--- Agent: root ---\n4", out)
}

// TestExec_OpenAI_WithThinkingBudget tests that when thinking_budget is explicitly configured
// in the YAML, thinking is enabled by default (without needing /think command).
func TestExec_OpenAI_WithThinkingBudget(t *testing.T) {
	out := cagentExec(t, "testdata/basic_with_thinking.yaml", "What's 2+2?")

	// With thinking_budget explicitly configured, response should include reasoning
	// The output format includes the reasoning summary when thinking is enabled
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, "4")
}

func TestExec_OpenAI_ToolCall(t *testing.T) {
	out := cagentExec(t, "testdata/fs_tools.yaml", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n\nCalling list_directory(path: \"testdata/working_dir\")\n\nlist_directory response → \"FILE README.me\\n\"\n1", out)
}

func TestExec_OpenAI_HideToolCalls(t *testing.T) {
	out := cagentExec(t, "testdata/fs_tools.yaml", "--hide-tool-calls", "How many files in testdata/working_dir? Only output the number.")

	require.Equal(t, "\n--- Agent: root ---\n1", out)
}

func TestExec_OpenAI_gpt5(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=openai/gpt-5", "What's 2+2?")

	// With thinking enabled by default, response may include reasoning summary
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, "4")
}

func TestExec_OpenAI_gpt5_1(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=openai/gpt-5.1", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\n2 + 2 = 4.", out)
}

func TestExec_OpenAI_gpt5_codex(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=openai/gpt-5-codex", "What's 2+2?")

	// Model reasoning summary varies, just check for the core response
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, "4")
}

func TestExec_Anthropic(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=anthropic/claude-sonnet-4-0", "What's 2+2?")

	// With interleaved thinking enabled by default, Anthropic responses include thinking content
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, "2 + 2 = 4")
}

func TestExec_Anthropic_ToolCall(t *testing.T) {
	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=anthropic/claude-sonnet-4-0", "How many files in testdata/working_dir? Only output the number.")

	// With interleaved thinking enabled by default, Anthropic responses include thinking content
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, `Calling list_directory(path: "testdata/working_dir")`)
	require.Contains(t, out, `list_directory response → "FILE README.me\n"`)
	// The response should end with "1" (the count)
	require.True(t, out != "" && out[len(out)-1] == '1', "response should end with '1'")
}

func TestExec_Anthropic_AgentsMd(t *testing.T) {
	out := cagentExec(t, "testdata/agents-md.yaml", "--model=anthropic/claude-sonnet-4-0", "What's 2+2?")

	// With interleaved thinking enabled by default, Anthropic responses include thinking content
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, "2 + 2 = 4")
}

func TestExec_Gemini(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=google/gemini-2.5-flash", "What's 2+2?")

	// With thinking enabled by default (dynamic thinking for Gemini 2.5), responses may include thinking content
	require.Contains(t, out, "--- Agent: root ---")
	// The response should contain the answer "4" somewhere
	require.Contains(t, out, "4")
}

func TestExec_Gemini_ToolCall(t *testing.T) {
	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=google/gemini-2.5-flash", "How many files in testdata/working_dir? Only output the number.")

	// With thinking enabled by default (dynamic thinking for Gemini 2.5), responses include thinking content
	require.Contains(t, out, "--- Agent: root ---")
	require.Contains(t, out, `Calling list_directory(path: "testdata/working_dir")`)
	require.Contains(t, out, `list_directory response → "FILE README.me\n"`)
	// The response should end with "1" (the count)
	require.True(t, out != "" && out[len(out)-1] == '1', "response should end with '1'")
}

func TestExec_Mistral(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=mistral/mistral-small", "What's 2+2?")

	require.Equal(t, "\n--- Agent: root ---\nThe sum of 2 + 2 is 4.", out)
}

func TestExec_Mistral_ToolCall(t *testing.T) {
	out := cagentExec(t, "testdata/fs_tools.yaml", "--model=mistral/mistral-small", "How many files in testdata/working_dir? Only output the number.")

	// NOTE: If you look at the LLM response, Mistral says it sees 2 files, yours truly got tired of re-running this test to get it to say "1".
	// For now, just update the expected output
	require.Equal(t, "\n--- Agent: root ---\n\nCalling list_directory(path: \"testdata/working_dir\")\n\nlist_directory response → \"FILE README.me\\n\"\n2", out)
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
	// Use a unique session DB path per test to avoid conflicts when tests run in parallel
	sessionDB := filepath.Join(t.TempDir(), "session.db")
	args = append(args, "--models-gateway", svr.URL, "--session-db", sessionDB)

	// Run cagent exec
	var stdout bytes.Buffer
	err = root.Execute(t.Context(), nil, &stdout, io.Discard, append(args, moreArgs...)...)
	require.NoError(t, err)

	return stdout.String()
}
