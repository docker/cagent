package environment

import (
	"context"

	"github.com/docker/cagent/pkg/desktop"
)

const DockerDesktopTokenEnv = "DOCKER_TOKEN"

type DockerDesktopProvider struct{}

func NewDockerDesktopProvider() *DockerDesktopProvider {
	return &DockerDesktopProvider{}
}

func (p *DockerDesktopProvider) Get(ctx context.Context, name string) string {
	if name != DockerDesktopTokenEnv {
		return ""
	}

	return desktop.GetToken(ctx)
}
