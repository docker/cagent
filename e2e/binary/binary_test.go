//go:build binary_required
// +build binary_required

package binary

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const binDir = "../../bin"

func TestHelpInAllExecMode(t *testing.T) {
	t.Run("cli plugin help", func(t *testing.T) {
		res, err := Exec("docker", "agent", "help")
		require.NoError(t, err)
		require.Contains(t, res.Stdout, "docker agent run ./agent.yaml")
	})

	t.Run("docker-agent help", func(t *testing.T) {
		res, err := Exec(binDir+"/docker-agent", "help")
		require.NoError(t, err)
		require.Contains(t, res.Stdout, "docker-agent run ./agent.yaml")
	})
}

func TestExecMissingKeys(t *testing.T) {
	t.Run("cli plugin exec", func(t *testing.T) {
		res, err := Exec("docker", "agent", "run", "--exec", "./test-agent.yaml")
		require.Error(t, err)
		require.Contains(t, res.Stderr, "environment variables must be set")
		require.Contains(t, res.Stderr, "OPENAI_API_KEY")
	})

	t.Run("docker-agent exec", func(t *testing.T) {
		res, err := Exec(binDir+"/docker-agent", "run", "--exec", "./test-agent.yaml")
		require.Error(t, err)
		require.Contains(t, res.Stderr, "environment variables must be set")
		require.Contains(t, res.Stderr, "OPENAI_API_KEY")
	})
}
