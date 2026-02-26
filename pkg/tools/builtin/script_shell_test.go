package builtin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/latest"
)

func TestNewScriptShellTool_Empty(t *testing.T) {
	tool, err := NewScriptShellTool(nil, nil)
	require.NoError(t, err)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	assert.Empty(t, allTools)
}

func TestNewScriptShellTool_ToolNoArg(t *testing.T) {
	shellTools := map[string]latest.ScriptShellToolConfig{
		"get_ip": {
			Description: "Get public IP",
		},
	}

	tool, err := NewScriptShellTool(shellTools, nil)
	require.NoError(t, err)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, allTools, 1)

	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {}
}`, string(schema))
}

func TestNewScriptShellTool_Tool(t *testing.T) {
	shellTools := map[string]latest.ScriptShellToolConfig{
		"github_user_repos": {
			Description: "List GitHub repositories of the provided user",
			Args: map[string]any{
				"username": map[string]any{
					"description": "GitHub username to get the repository list for",
					"type":        "string",
				},
			},
			Required: []string{"username"},
		},
	}

	tool, err := NewScriptShellTool(shellTools, nil)
	require.NoError(t, err)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, allTools, 1)

	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"username": {
			"description": "GitHub username to get the repository list for",
			"type": "string"
		}
	},
	"required": ["username"]
}`, string(schema))
}

func TestNewScriptShellTool_Typo(t *testing.T) {
	shellTools := map[string]latest.ScriptShellToolConfig{
		"docker_images": {
			Description: "List running Docker containers",
			Cmd:         "docker images $image",
			Args: map[string]any{
				"img": map[string]any{
					"description": "Docker image to list",
					"type":        "string",
				},
			},
			Required: []string{"img"},
		},
	}

	tool, err := NewScriptShellTool(shellTools, nil)
	require.Nil(t, tool)
	require.ErrorContains(t, err, "tool 'docker_images' uses undefined args: [image]")
}

func TestNewScriptShellTool_MissingRequired(t *testing.T) {
	shellTools := map[string]latest.ScriptShellToolConfig{
		"docker_images": {
			Description: "List running Docker containers",
			Cmd:         "docker images $image",
			Args: map[string]any{
				"image": map[string]any{
					"description": "Docker image to list",
					"type":        "string",
				},
			},
			Required: []string{"img"},
		},
	}

	tool, err := NewScriptShellTool(shellTools, nil)
	require.Nil(t, tool)
	require.ErrorContains(t, err, "tool 'docker_images' has required arg 'img' which is not defined in args")
}

func TestNewScriptShellTool_ArgWithoutType(t *testing.T) {
	shellTools := map[string]latest.ScriptShellToolConfig{
		"greet": {
			Description: "Greet someone",
			Cmd:         "echo Hello $name",
			Args: map[string]any{
				"name": map[string]any{
					"description": "Name to greet",
				},
			},
			Required: []string{"name"},
		},
	}

	tool, err := NewScriptShellTool(shellTools, nil)
	require.NoError(t, err)

	allTools, err := tool.Tools(t.Context())
	require.NoError(t, err)
	assert.Len(t, allTools, 1)

	schema, err := json.Marshal(allTools[0].Parameters)
	require.NoError(t, err)
	assert.JSONEq(t, `{
	"type": "object",
	"properties": {
		"name": {
			"description": "Name to greet",
			"type": "string"
		}
	},
	"required": ["name"]
}`, string(schema))
}
