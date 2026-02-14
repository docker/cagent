package root

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/session"
)

// TestSessionOptsConsistency verifies that buildSessionOpts produces the same
// options for both initial and spawned sessions when given the same flags.
// This prevents regressions where a new session option is added to one code
// path but not the other.
func TestSessionOptsConsistency(t *testing.T) {
	t.Parallel()

	workingDir := "/projects/app"

	flags := runExecFlags{
		autoApprove:     true,
		hideToolResults: true,
		splitDiffView:   false,
	}

	maxIterations := 25
	thinking := true

	opts := flags.buildSessionOpts(maxIterations, thinking, workingDir)

	sess := session.New(opts...)

	assert.Equal(t, maxIterations, sess.MaxIterations)
	assert.True(t, sess.ToolsApproved)
	assert.True(t, sess.HideToolResults)
	assert.True(t, sess.Thinking)
	assert.False(t, sess.GetSplitDiffView())
	assert.Equal(t, workingDir, sess.WorkingDir)
	assert.Equal(t, []string{workingDir}, sess.AllowedDirectories())
}

// TestSessionOptsEmptyWorkingDir verifies the builder handles empty working dir.
func TestSessionOptsEmptyWorkingDir(t *testing.T) {
	t.Parallel()

	flags := runExecFlags{}

	opts := flags.buildSessionOpts(10, false, "")
	sess := session.New(opts...)

	assert.Empty(t, sess.WorkingDir)
	assert.Nil(t, sess.AllowedDirectories())
	assert.False(t, sess.ToolsApproved)
	assert.False(t, sess.HideToolResults)
}
