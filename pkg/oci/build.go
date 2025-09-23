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
	"github.com/docker/cagent/pkg/secrets"
)

//go:embed Dockerfile.template
var dockerfileTemplate string

type Options struct {
	DryRun  bool
	Push    bool
	NoCache bool
	Pull    bool
}

func BuildDockerImage(ctx context.Context, agentFilePath, dockerImageName string, opts Options) error {
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

	// Analyze the config to find which secrets are needed
	modelSecrets := secrets.GatherEnvVarsForModels(cfg)
	toolSecrets, err := secrets.GatherEnvVarsForTools(ctx, cfg)
	if err != nil {
		return err
	}

	// Find which base image to use
	baseImage := "docker/cagent"
	if baseImageOverride := os.Getenv("CAGENT_BASE_IMAGE"); baseImageOverride != "" {
		baseImage = baseImageOverride
	}

	// Generate the Dockerfile
	var dockerfileBuf bytes.Buffer

	tpl := template.Must(template.New("Dockerfile").Parse(dockerfileTemplate))
	if err := tpl.Execute(&dockerfileBuf, map[string]any{
		"BaseImage":    baseImage,
		"AgentConfig":  string(agentYaml),
		"BuildDate":    time.Now().UTC().Format(time.RFC3339),
		"Description":  cfg.Agents["root"].Description,
		"Metadata":     cfg.Metadata,
		"ModelSecrets": strings.Join(modelSecrets, ","),
		"ToolSecrets":  strings.Join(toolSecrets, ","),
	}); err != nil {
		return err
	}

	dockerfile := dockerfileBuf.String()
	if opts.DryRun {
		fmt.Println(dockerfile)
		return nil
	}

	// Run docker build
	buildArgs := []string{"build"}
	if opts.NoCache {
		buildArgs = append(buildArgs, "--no-cache")
	}
	if opts.Pull {
		buildArgs = append(buildArgs, "--pull")
	}
	if dockerImageName != "" {
		buildArgs = append(buildArgs, "-t", dockerImageName)
		if opts.Push {
			buildArgs = append(buildArgs, "--push", "--platform", "linux/amd64,linux/arm64")
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
