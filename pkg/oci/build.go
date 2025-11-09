package oci

import (
	"bytes"
	"context"
	_ "embed"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/docker/cagent/pkg/cli"
	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/filesystem"
)

//go:embed Dockerfile.template
var dockerfileTemplate string

type Options struct {
	DryRun  bool
	Push    bool
	NoCache bool
	Pull    bool
}

func BuildDockerImage(ctx context.Context, out *cli.Printer, agentFilePath string, fs filesystem.FS, dockerImageName string, opts Options) error {
	cfg, err := config.LoadConfig(agentFilePath, fs)
	if err != nil {
		return err
	}

	// Compute the canonical form of the config
	canonical, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	// Analyze the config to find which secrets are needed
	modelSecrets := config.GatherEnvVarsForModels(cfg)
	toolSecrets, err := config.GatherEnvVarsForTools(ctx, cfg)
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
		"AgentConfig":  string(canonical),
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
		out.Println(dockerfile)
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
