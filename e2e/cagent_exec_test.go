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

func TestExec_BasicOpenAI(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "Who's djordje?")

	require.Equal(t, `
--- Agent: root ---

Djordje is a common Serbian given name. It may refer to different individuals depending on the context. If you provide more information or details, I can help you identify the specific Djordje you are referring to.`,
		out)
}

func TestExec_BasicAnthropic(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=anthropic/claude-sonnet-4-0", "Who's djordje?. Be super concise.")

	require.Equal(t, `
--- Agent: root ---

I need more context. There are many people named Djordje (a Serbian name). Could you specify which Djordje you're asking about?`,
		out)
}

func TestExec_BasicGemini(t *testing.T) {
	out := cagentExec(t, "testdata/basic.yaml", "--model=google/gemini-2.5-flash", "Who's djordje?. Be super concise.")

	require.Equal(t, `
--- Agent: root ---

Serbian equivalent of the name George.`,
		out)
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
