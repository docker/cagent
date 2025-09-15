package oci

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/docker/cagent/pkg/config"
)

//go:embed Dockerfile.template
var dockerfileTemplate string

func BuildDockerImage(ctx context.Context, agentFilePath, dockerImageName string, push bool) error {
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

	// Make sure the config is compatible with `cagent build`
	for _, agent := range cfg.Agents {
		for i := range agent.Toolsets {
			toolSet := agent.Toolsets[i]

			if toolSet.Type == "mcp" && toolSet.Command != "" {
				return fmt.Errorf("toolset with command \"%s\" can't be used in `cagent build`", toolSet.Command)
			}
		}
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
		"Metadata":    cfg.Metadata,
		"Models":      strings.Join(modelNames, ","),
	}); err != nil {
		return err
	}

	dockerfile := dockerfileBuf.String()
	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		fmt.Println(dockerfile)
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
