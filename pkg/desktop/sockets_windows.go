//go:build !no_docker_desktop && windows
// +build !no_docker_desktop,windows

package desktop

import (
	"errors"
	"os"
)

func getDockerDesktopPaths() (DockerDesktopPaths, error) {
	appData := os.Getenv("ProgramData")
	if appData == "" {
		return DockerDesktopPaths{}, errors.New("unable to get 'ProgramData'")
	}

	return DockerDesktopPaths{
		BackendSocket: `\\.\pipe\dockerBackendApiServer`,
	}, nil
}
