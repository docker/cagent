package v2

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/types"
)

func TestCommandsUnmarshal_Map(t *testing.T) {
	var c types.Commands
	input := []byte(`
df: "check disk"
ls: "list files"
`)
	err := yaml.Unmarshal(input, &c)
	require.NoError(t, err)
	require.Equal(t, "check disk", c["df"])
	require.Equal(t, "list files", c["ls"])
}

func TestCommandsUnmarshal_List(t *testing.T) {
	var c types.Commands
	input := []byte(`
- df: "check disk"
- ls: "list files"
`)
	err := yaml.Unmarshal(input, &c)
	require.NoError(t, err)
	require.Equal(t, "check disk", c["df"])
	require.Equal(t, "list files", c["ls"])
}
