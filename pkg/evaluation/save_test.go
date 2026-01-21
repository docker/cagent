package evaluation

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/session"
)

func TestSaveWithCustomFilename(t *testing.T) {
	// Create a temporary directory and change to it
	t.Chdir(t.TempDir())

	// Create a test session
	sess := session.New()
	sess.ID = "test-session-id"

	// Test 1: Save with custom filename
	evalFile, err := Save(sess, "my-custom-eval")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("evals", "my-custom-eval.json"), evalFile)
	require.FileExists(t, evalFile)

	// Test 2: Save without filename (should use session ID)
	evalFile2, err := Save(sess, "")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("evals", sess.ID+".json"), evalFile2)
	require.FileExists(t, evalFile2)

	// Test 3: Save with same filename (should add _1 suffix)
	evalFile3, err := Save(sess, "my-custom-eval")
	require.NoError(t, err)
	require.Equal(t, filepath.Join("evals", "my-custom-eval_1.json"), evalFile3)
	require.FileExists(t, evalFile3)
}
