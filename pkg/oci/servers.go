package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Servers struct {
	MCPServers map[string]Server `json:"mcpServers,omitempty"`
}

type Server struct {
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
}

func mcpServerArgs(ctx context.Context, ref string, config any) ([]string, error) {
	args := []string{"run", "--rm", "-i", "--init"}

	name := stripDockerPrefix(ref)

	mcpServer, err := readMCPServerFromCatalog(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("error getting image for MCP server: %w", err)
	}

	mcpImageConfig, err := inspectDockerImage(ctx, mcpServer.Image)
	if err != nil {
		return nil, fmt.Errorf("error inspecting image: %w", err)
	}

	// TODO(dga): handle config for real. Probably using the gateway's code
	for _, volume := range mcpServer.Volumes {
		volume := strings.ReplaceAll(volume, "{{ast-grep.path|volume-target}}", config.(map[string]any)["path"].(string))
		args = append(args, "-v", volume)
	}

	for _, env := range mcpServer.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", env.Name, env.Value))
	}

	args = append(args, mcpServer.Image)
	args = append(args, mcpImageConfig.Entrypoint...)
	args = append(args, mcpImageConfig.Cmd...)

	// TODO(dga): handle config for real. Probably using the gateway's code
	if ref == "docker:ast-grep" {
		args = append(args, config.(map[string]any)["path"].(string))
	}

	return args, nil
}

func inspectDockerImage(ctx context.Context, imageRef string) (Config, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format=json", imageRef).Output()
	if err != nil {
		if err := exec.CommandContext(ctx, "docker", "pull", imageRef).Run(); err != nil {
			return Config{}, fmt.Errorf("error pulling image: %w", err)
		}

		out, err = exec.CommandContext(ctx, "docker", "inspect", "--format=json", imageRef).Output()
	}
	if err != nil {
		return Config{}, fmt.Errorf("error inspecting image: %w", err)
	}

	var imageConfigs []ImageConfig
	if err := json.Unmarshal(out, &imageConfigs); err != nil {
		return Config{}, fmt.Errorf("error parsing config JSON: %w", err)
	}

	if len(imageConfigs) != 1 {
		return Config{}, fmt.Errorf("expected one image config, got %d", len(imageConfigs))
	}

	return imageConfigs[0].Config, nil
}

func stripDockerPrefix(name string) string {
	if after, ok := strings.CutPrefix(name, "docker:"); ok {
		return after
	}
	return name
}

func readMCPServerFromCatalog(ctx context.Context, serverName string) (MCPServer, error) {
	catalog, err := readCatalog(ctx)
	if err != nil {
		return MCPServer{}, err
	}

	server, ok := catalog[serverName]
	if !ok {
		return MCPServer{}, fmt.Errorf("MCP server %q not found in catalog", serverName)
	}

	return server, nil
}
