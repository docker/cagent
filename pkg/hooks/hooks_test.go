package hooks

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHookGetTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		hook     Hook
		expected time.Duration
	}{
		{
			name:     "default timeout",
			hook:     Hook{},
			expected: 60 * time.Second,
		},
		{
			name:     "zero timeout uses default",
			hook:     Hook{Timeout: 0},
			expected: 60 * time.Second,
		},
		{
			name:     "negative timeout uses default",
			hook:     Hook{Timeout: -1},
			expected: 60 * time.Second,
		},
		{
			name:     "custom timeout",
			hook:     Hook{Timeout: 30},
			expected: 30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.hook.GetTimeout())
		})
	}
}

func TestConfigIsEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "empty config",
			config:   Config{},
			expected: true,
		},
		{
			name: "with pre_tool_use",
			config: Config{
				PreToolUse: []MatcherConfig{{Matcher: "*", Hooks: []Hook{}}},
			},
			expected: false,
		},
		{
			name: "with post_tool_use",
			config: Config{
				PostToolUse: []MatcherConfig{{Matcher: "*"}},
			},
			expected: false,
		},
		{
			name: "with session_start",
			config: Config{
				SessionStart: []Hook{{Type: HookTypeCommand}},
			},
			expected: false,
		},
		{
			name: "with session_end",
			config: Config{
				SessionEnd: []Hook{{Type: HookTypeCommand}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.config.IsEmpty())
		})
	}
}

func TestInputToJSON(t *testing.T) {
	t.Parallel()

	input := &Input{
		SessionID:     "sess-123",
		Cwd:           "/tmp",
		HookEventName: EventPreToolUse,
		ToolName:      "shell",
		ToolUseID:     "tool-456",
		ToolInput: map[string]any{
			"cmd": "ls -la",
			"cwd": ".",
		},
	}

	data, err := input.ToJSON()
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "sess-123", parsed["session_id"])
	assert.Equal(t, "/tmp", parsed["cwd"])
	assert.Equal(t, "pre_tool_use", parsed["hook_event_name"])
	assert.Equal(t, "shell", parsed["tool_name"])
	assert.Equal(t, "tool-456", parsed["tool_use_id"])
}

func TestOutputShouldContinue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   Output
		expected bool
	}{
		{
			name:     "nil continue defaults to true",
			output:   Output{},
			expected: true,
		},
		{
			name:     "continue true",
			output:   Output{Continue: new(true)},
			expected: true,
		},
		{
			name:     "continue false",
			output:   Output{Continue: new(false)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.output.ShouldContinue())
		})
	}
}

func TestOutputIsBlocked(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   Output
		expected bool
	}{
		{
			name:     "empty decision",
			output:   Output{},
			expected: false,
		},
		{
			name:     "block decision",
			output:   Output{Decision: "block"},
			expected: true,
		},
		{
			name:     "allow decision",
			output:   Output{Decision: "allow"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.output.IsBlocked())
		})
	}
}

func TestNewExecutor(t *testing.T) {
	t.Parallel()

	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "shell|edit_file",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "echo pre"},
				},
			},
		},
	}

	exec := NewExecutor(config, "/tmp", []string{"FOO=bar"})
	require.NotNil(t, exec)
	assert.True(t, exec.HasPreToolUseHooks())
	assert.False(t, exec.HasPostToolUseHooks())
	assert.False(t, exec.HasSessionStartHooks())
	assert.False(t, exec.HasSessionEndHooks())
}

func TestExecutorNilConfig(t *testing.T) {
	t.Parallel()

	exec := NewExecutor(nil, "/tmp", nil)
	require.NotNil(t, exec)
	assert.False(t, exec.HasPreToolUseHooks())
}

func TestCompiledMatcherMatchTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		matcher  string
		toolName string
		expected bool
	}{
		{
			name:     "wildcard matches any",
			matcher:  "*",
			toolName: "shell",
			expected: true,
		},
		{
			name:     "empty matcher matches any",
			matcher:  "",
			toolName: "shell",
			expected: true,
		},
		{
			name:     "exact match",
			matcher:  "shell",
			toolName: "shell",
			expected: true,
		},
		{
			name:     "exact match fails",
			matcher:  "shell",
			toolName: "edit_file",
			expected: false,
		},
		{
			name:     "alternation match first",
			matcher:  "shell|edit_file",
			toolName: "shell",
			expected: true,
		},
		{
			name:     "alternation match second",
			matcher:  "shell|edit_file",
			toolName: "edit_file",
			expected: true,
		},
		{
			name:     "alternation no match",
			matcher:  "shell|edit_file",
			toolName: "write_file",
			expected: false,
		},
		{
			name:     "regex pattern",
			matcher:  "mcp__.*",
			toolName: "mcp__github_list_repos",
			expected: true,
		},
		{
			name:     "regex pattern no match",
			matcher:  "mcp__.*",
			toolName: "shell",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := &Config{
				PreToolUse: []MatcherConfig{
					{Matcher: tt.matcher, Hooks: []Hook{{Type: HookTypeCommand, Command: "echo test"}}},
				},
			}
			exec := NewExecutor(config, "/tmp", nil)
			require.Len(t, exec.preToolUseMatchers, 1)
			assert.Equal(t, tt.expected, exec.preToolUseMatchers[0].matchTool(tt.toolName))
		})
	}
}

func TestExecutePreToolUseWithEchoCommand(t *testing.T) {
	t.Parallel()

	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "echo 'test'", Timeout: 5},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		ToolName:  "shell",
		ToolUseID: "test-id",
	}

	result, err := exec.ExecutePreToolUse(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestExecutePreToolUseBlockingExitCode(t *testing.T) {
	t.Parallel()

	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "exit 2", Timeout: 5},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		ToolName:  "shell",
		ToolUseID: "test-id",
	}

	result, err := exec.ExecutePreToolUse(t.Context(), input)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Equal(t, 2, result.ExitCode)
}

func TestExecutePreToolUseNoMatchingHooks(t *testing.T) {
	t.Parallel()

	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "edit_file",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "exit 2", Timeout: 5},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		ToolName:  "shell", // Doesn't match "edit_file"
		ToolUseID: "test-id",
	}

	result, err := exec.ExecutePreToolUse(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, result.Allowed) // Should be allowed since no hooks matched
}

func TestExecutePreToolUseWithJSONOutput(t *testing.T) {
	t.Parallel()

	jsonOutput := `{"decision":"block","reason":"Tool not allowed"}`
	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "echo '" + jsonOutput + "'", Timeout: 5},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		ToolName:  "shell",
		ToolUseID: "test-id",
	}

	result, err := exec.ExecutePreToolUse(t.Context(), input)
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.Message, "Tool not allowed")
}

func TestExecutePostToolUse(t *testing.T) {
	t.Parallel()

	config := &Config{
		PostToolUse: []MatcherConfig{
			{
				Matcher: "shell",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "echo 'post-hook'", Timeout: 5},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID:    "test-session",
		ToolName:     "shell",
		ToolUseID:    "test-id",
		ToolResponse: "command output",
	}

	result, err := exec.ExecutePostToolUse(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestExecuteSessionStart(t *testing.T) {
	t.Parallel()

	config := &Config{
		SessionStart: []Hook{
			{Type: HookTypeCommand, Command: "echo 'session starting'", Timeout: 5},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		Source:    "startup",
	}

	result, err := exec.ExecuteSessionStart(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
	assert.Contains(t, result.AdditionalContext, "session starting")
}

func TestExecuteSessionEnd(t *testing.T) {
	t.Parallel()

	config := &Config{
		SessionEnd: []Hook{
			{Type: HookTypeCommand, Command: "echo 'session ending'", Timeout: 5},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		Reason:    "logout",
	}

	result, err := exec.ExecuteSessionEnd(t.Context(), input)
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestExecuteHooksWithContextCancellation(t *testing.T) {
	t.Parallel()

	config := &Config{
		PreToolUse: []MatcherConfig{
			{
				Matcher: "*",
				Hooks: []Hook{
					{Type: HookTypeCommand, Command: "sleep 10", Timeout: 30},
				},
			},
		},
	}

	exec := NewExecutor(config, t.TempDir(), nil)
	input := &Input{
		SessionID: "test-session",
		ToolName:  "shell",
		ToolUseID: "test-id",
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	result, err := exec.ExecutePreToolUse(ctx, input)
	require.NoError(t, err)
	// Should be allowed because the hook timed out (non-blocking error)
	assert.True(t, result.Allowed)
}
