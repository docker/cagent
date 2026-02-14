package builtin

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/tools"
)

func TestNewShellTool(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/bash", tool.handler.shell)

	t.Setenv("SHELL", "")
	tool = NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	assert.NotNil(t, tool)
	assert.NotNil(t, tool.handler)
	assert.Equal(t, "/bin/sh", tool.handler.shell, "Should default to /bin/sh when SHELL is not set")
}

func TestShellTool_HandlerEcho(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	result, err := tool.handler.RunShell(t.Context(), RunShellArgs{
		Cmd: "echo 'hello world'",
		Cwd: "",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "hello world")
}

func TestShellTool_HandlerWithCwd(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)
	tmpDir := t.TempDir()

	result, err := tool.handler.RunShell(t.Context(), RunShellArgs{
		Cmd: "pwd",
		Cwd: tmpDir,
	})
	require.NoError(t, err)
	// The output might contain extra newlines or other characters,
	// so we just check if it contains the temp dir path
	assert.Contains(t, result.Output, tmpDir)
}

func TestShellTool_HandlerError(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	result, err := tool.handler.RunShell(t.Context(), RunShellArgs{
		Cmd: "command_that_does_not_exist",
		Cwd: "",
	})
	require.NoError(t, err, "Handler should not return an error")
	assert.Contains(t, result.Output, "Error executing command")
}

func TestShellTool_OutputSchema(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		assert.NotNil(t, tool.OutputSchema)
	}
}

func TestShellTool_ParametersAreObjects(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, allTools)

	for _, tool := range allTools {
		m, err := tools.SchemaToMap(tool.Parameters)
		require.NoError(t, err)
		assert.Equal(t, "object", m["type"])
	}
}

// Minimal tests for background job features
func TestShellTool_RunBackgroundJob(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)
	err := tool.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tool.Stop(t.Context())
	})

	result, err := tool.handler.RunShellBackground(t.Context(), RunShellBackgroundArgs{Cmd: "echo test"})
	require.NoError(t, err)
	assert.Contains(t, result.Output, "Background job started with ID:")
}

func TestShellTool_ListBackgroundJobs(t *testing.T) {
	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)
	err := tool.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = tool.Stop(t.Context())
	})

	// Start a background job first
	_, err = tool.handler.RunShellBackground(t.Context(), RunShellBackgroundArgs{Cmd: "echo test"})
	require.NoError(t, err)

	// No need to wait - ListBackgroundJobs shows jobs regardless of status
	listResult, err := tool.handler.ListBackgroundJobs(t.Context(), nil)

	require.NoError(t, err)
	assert.Contains(t, listResult.Output, "Background Jobs:")
	assert.Contains(t, listResult.Output, "ID: job_")
}

func TestParseSandboxPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		wantPath string
		wantMode string
	}{
		{input: ".", wantPath: ".", wantMode: "rw"},
		{input: "/tmp", wantPath: "/tmp", wantMode: "rw"},
		{input: "./src", wantPath: "./src", wantMode: "rw"},
		{input: "/tmp:ro", wantPath: "/tmp", wantMode: "ro"},
		{input: "./config:ro", wantPath: "./config", wantMode: "ro"},
		{input: "/data:rw", wantPath: "/data", wantMode: "rw"},
		{input: "./secrets:ro", wantPath: "./secrets", wantMode: "ro"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			path, mode := parseSandboxPath(tt.input)
			assert.Equal(t, tt.wantPath, path)
			assert.Equal(t, tt.wantMode, mode)
		})
	}
}

func TestShellTool_SandboxInstructions(t *testing.T) {
	t.Parallel()

	workingDir := "/workspace/project"
	sandboxConfig := &latest.SandboxConfig{
		Image: "alpine:latest",
		Paths: []string{
			".",
			"/tmp",
			"/home/user:ro",
		},
	}

	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: workingDir}}, sandboxConfig)

	instructions := tool.Instructions()

	// Check that the sandbox note is included
	assert.Contains(t, instructions, "For sandboxing reasons, all shell commands run inside a Linux container")

	// Check that the native instructions are included
	assert.Contains(t, instructions, "Shell Tool Usage Guide")

	// Verify that mounted paths section is NOT included
	assert.NotContains(t, instructions, "## Mounted Paths")
	assert.NotContains(t, instructions, "The following paths are accessible in the sandbox:")
}

func TestShellTool_NativeInstructions(t *testing.T) {
	t.Parallel()

	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: t.TempDir()}}, nil)

	instructions := tool.Instructions()

	// Check that native instructions are returned (not sandbox)
	assert.Contains(t, instructions, "Shell Tool Usage Guide")
	assert.NotContains(t, instructions, "Sandbox Mode")
	assert.NotContains(t, instructions, "## Mounted Paths")
}

func TestIsValidEnvVarName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		valid bool
	}{
		{"HOME", true},
		{"USER", true},
		{"PATH", true},
		{"_private", true},
		{"MY_VAR_123", true},
		{"a", true},
		{"A", true},
		{"_", true},
		{"", false},
		{"123", false},
		{"1VAR", false},
		{"VAR-NAME", false},
		{"VAR.NAME", false},
		{"VAR NAME", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isValidEnvVarName(tt.name)
			assert.Equal(t, tt.valid, result, "isValidEnvVarName(%q)", tt.name)
		})
	}
}

func TestIsProcessRunning(t *testing.T) {
	t.Parallel()

	// Current process should be running
	assert.True(t, isProcessRunning(os.Getpid()), "Current process should be running")

	// Non-existent PID should not be running (using a very high PID unlikely to exist)
	assert.False(t, isProcessRunning(999999999), "Very high PID should not be running")
}

func TestResolveWorkDir(t *testing.T) {
	t.Parallel()

	workingDir := "/configured/project"
	h := &shellHandler{workingDir: workingDir}

	tests := []struct {
		name     string
		cwd      string
		expected string
	}{
		{name: "empty defaults to workingDir", cwd: "", expected: workingDir},
		{name: "dot defaults to workingDir", cwd: ".", expected: workingDir},
		{name: "absolute path unchanged", cwd: "/tmp/other", expected: "/tmp/other"},
		{name: "relative path joined with workingDir", cwd: "src/pkg", expected: "/configured/project/src/pkg"},
		{name: "relative with dot prefix", cwd: "./subdir", expected: "/configured/project/subdir"},
		{name: "relative with parent traversal", cwd: "../sibling", expected: "/configured/sibling"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, h.resolveWorkDir(tt.cwd))
		})
	}
}

func TestShellTool_RelativeCwdResolvesAgainstWorkingDir(t *testing.T) {
	// Create a directory structure: workingDir/subdir/
	workingDir := t.TempDir()
	subdir := workingDir + "/subdir"
	require.NoError(t, os.Mkdir(subdir, 0o755))

	tool := NewShellTool(nil, &config.RuntimeConfig{Config: config.Config{WorkingDir: workingDir}}, nil)

	result, err := tool.handler.RunShell(t.Context(), RunShellArgs{
		Cmd: "pwd",
		Cwd: "subdir",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Output, subdir,
		"relative cwd must resolve against the configured workingDir, not the process cwd")
}
