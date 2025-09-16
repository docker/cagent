package oci

import (
	"context"
	"testing"

	v2 "github.com/docker/cagent/pkg/config/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func setupMCPCatalog(t *testing.T, name string, description MCPServer) {
	t.Helper()

	oldCatalog := readCatalog
	t.Cleanup(func() { readCatalog = oldCatalog })

	readCatalog = func(context.Context) (Catalog, error) {
		return map[string]MCPServer{
			name: description,
		}, nil
	}
}

func parseToolSet(t *testing.T, yamlContent string) v2.Toolset {
	t.Helper()

	var toolSet v2.Toolset
	err := yaml.Unmarshal([]byte(yamlContent), &toolSet)
	require.NoError(t, err)

	return toolSet
}

func TestMCPToolSetToServerAstGrep(t *testing.T) {
	setupMCPCatalog(t, "ast-grep", MCPServer{
		Image:   "mcp/ast-grep@sha256:fb6a7d8fd0f70f8e4103c7386ef09d6eef7c57086b725ed7b4350f5fe7a28981",
		Volumes: []string{"{{ast-grep.path|volume-target}}:/src"},
		Config: []MCPConfig{{
			Properties: map[string]Property{
				"path": {Type: "string"},
			},
		}},
	})

	toolSet := parseToolSet(t, `type: mcp
ref: docker:ast-grep
config:
  path: .
`)

	server, err := mcpToolSetToServer(t.Context(), &toolSet)
	require.NoError(t, err)

	assert.Equal(t, "docker", server.Command)
	assert.Equal(t, []string{"run", "--rm", "-i", "--init", "-v", ".:/src", "mcp/ast-grep@sha256:fb6a7d8fd0f70f8e4103c7386ef09d6eef7c57086b725ed7b4350f5fe7a28981", "/mcp-server", "."}, server.Args)
	assert.Empty(t, server.Env)
}

func TestMCPToolSetToServerContext7(t *testing.T) {
	setupMCPCatalog(t, "context7", MCPServer{
		Image: "mcp/context7@sha256:1174e6a29634a83b2be93ac1fefabf63265f498c02c72201fe3464e687dd8836",
		Env: []Env{{
			Name:  "MCP_TRANSPORT",
			Value: "stdio",
		}},
	})

	toolSet := parseToolSet(t, `type: mcp
ref: docker:context7
`)

	server, err := mcpToolSetToServer(t.Context(), &toolSet)
	require.NoError(t, err)

	assert.Equal(t, "docker", server.Command)
	assert.Equal(t, []string{"run", "--rm", "-i", "--init", "-e", "MCP_TRANSPORT=stdio", "mcp/context7@sha256:1174e6a29634a83b2be93ac1fefabf63265f498c02c72201fe3464e687dd8836", "docker-entrypoint.sh", "node", "dist/index.js"}, server.Args)
	assert.Empty(t, server.Env)
}
