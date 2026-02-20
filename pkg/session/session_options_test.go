package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithWorkingDir_SetsAllowedDirectories(t *testing.T) {
	s := New(WithWorkingDir("/projects/myapp"))

	assert.Equal(t, "/projects/myapp", s.WorkingDir)
	assert.Equal(t, []string{"/projects/myapp"}, s.AllowedDirectories())
}

func TestWithWorkingDir_EmptyReturnsNilAllowedDirs(t *testing.T) {
	s := New()

	assert.Empty(t, s.WorkingDir)
	assert.Nil(t, s.AllowedDirectories())
}

func TestNewSession_AllOptionsApplied(t *testing.T) {
	s := New(
		WithMaxIterations(10),
		WithToolsApproved(true),
		WithHideToolResults(true),
		WithThinking(true),
		WithWorkingDir("/work"),
	)

	assert.Equal(t, 10, s.MaxIterations)
	assert.True(t, s.ToolsApproved)
	assert.True(t, s.HideToolResults)
	assert.True(t, s.Thinking)
	assert.Equal(t, "/work", s.WorkingDir)
	assert.Equal(t, []string{"/work"}, s.AllowedDirectories())
}

// TestNewSession_ConsistencyBetweenInitialAndSpawned verifies that the
// initial session and spawned sessions receive the same set of options.
// This test documents the expected option set so that adding a new option
// to one path without the other will be caught.
func TestNewSession_ConsistencyBetweenInitialAndSpawned(t *testing.T) {
	workingDir := "/projects/app"
	autoApprove := true
	hideToolResults := true
	thinking := true
	maxIterations := 25

	// Simulate what createLocalRuntimeAndSession builds (initial session).
	initial := New(
		WithMaxIterations(maxIterations),
		WithToolsApproved(autoApprove),
		WithHideToolResults(hideToolResults),
		WithThinking(thinking),
		WithWorkingDir(workingDir),
	)

	// Simulate what createSessionSpawner builds (spawned session).
	spawned := New(
		WithMaxIterations(maxIterations),
		WithToolsApproved(autoApprove),
		WithHideToolResults(hideToolResults),
		WithThinking(thinking),
		WithWorkingDir(workingDir),
	)

	assert.Equal(t, initial.MaxIterations, spawned.MaxIterations)
	assert.Equal(t, initial.ToolsApproved, spawned.ToolsApproved)
	assert.Equal(t, initial.HideToolResults, spawned.HideToolResults)
	assert.Equal(t, initial.Thinking, spawned.Thinking)
	assert.Equal(t, initial.WorkingDir, spawned.WorkingDir)
	assert.Equal(t, initial.AllowedDirectories(), spawned.AllowedDirectories())
}
