package oci

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/docker/cagent/pkg/config"
	v2 "github.com/docker/cagent/pkg/config/v2"
)

//go:embed Dockerfile.template
var dockerfileTemplate string

func BuildDockerImage(ctx context.Context, agentFilePath, dockerImageName string, dryRun, push bool) error {
	agentYaml, err := os.ReadFile(agentFilePath)
	if err != nil {
		return err
	}

	fileName := filepath.Base(agentFilePath)
	parentDir := filepath.Dir(agentFilePath)
	cfg, err := config.LoadConfigSecure(fileName, parentDir)
	if err != nil {
		return err
	}

	// Collect information about MCP servers
	servers := Servers{
		MCPServers: map[string]Server{},
	}

	// Make sure the config is compatible with `cagent build`
	for _, agent := range cfg.Agents {
		for i := range agent.Toolsets {
			toolSet := agent.Toolsets[i]
			if toolSet.Type != "mcp" {
				continue
			}

			if toolSet.Command != "" {
				return fmt.Errorf("toolset with command \"%s\" can't be used in `cagent build`", toolSet.Command)
			}

			server, err := mcpToolSetToServer(ctx, &toolSet)
			if err != nil {
				return err
			}
			servers.MCPServers[toolSet.Ref] = server
		}
	}

	// Build the content of an optional servers.json
	var serversJson string
	if len(servers.MCPServers) > 0 {
		data, err := json.MarshalIndent(servers, "", "  ")
		if err != nil {
			return err
		}

		serversJson = string(data)
	}

	// Analyze the config to find which secrets are needed
	modelNames := config.GatherModelNames(cfg)
	mcpServers := config.GatherMCPServerReferences(cfg)

	// Generate the Dockerfile
	var dockerfileBuf bytes.Buffer

	tpl := template.Must(template.New("Dockerfile").Parse(dockerfileTemplate))
	if err := tpl.Execute(&dockerfileBuf, map[string]any{
		"AgentConfig": string(agentYaml),
		"BuildDate":   time.Now().UTC().Format(time.RFC3339),
		"Description": cfg.Agents["root"].Description,
		"McpServers":  strings.Join(mcpServers, ","),
		"ServersJson": serversJson,
		"Metadata":    cfg.Metadata,
		"Models":      strings.Join(modelNames, ","),
	}); err != nil {
		return err
	}

	dockerfile := dockerfileBuf.String()
	if dryRun {
		fmt.Println(dockerfile)
		return nil
	}

	// Run docker build
	buildArgs := []string{"build"}
	if dockerImageName != "" {
		buildArgs = append(buildArgs, "-t", dockerImageName)
		if push {
			buildArgs = append(buildArgs, "--push")
		}
	}
	buildArgs = append(buildArgs, "-")

	buildCmd := exec.CommandContext(ctx, "docker", buildArgs...)
	buildCmd.Stdin = strings.NewReader(dockerfile)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	slog.Debug("running docker build", "args", buildArgs)

	return buildCmd.Run()
}

func mcpToolSetToServer(ctx context.Context, toolSet *v2.Toolset) (Server, error) {
	args, err := mcpServerArgs(ctx, toolSet.Ref, toolSet.Config)
	if err != nil {
		return Server{}, err
	}

	// TODO(dga): support the config part (probably by appending to the args or by adding env variables)
	//   - type: mcp
	//     ref: docker:ast-grep
	//     config:
	//       path: .
	// TODO(dga): What's the actual command?
	return Server{
		Command: "docker",
		Args:    args,
	}, nil
}
