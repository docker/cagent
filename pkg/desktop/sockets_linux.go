//go:build !no_docker_desktop && linux
// +build !no_docker_desktop,linux

package desktop

import (
	"errors"
	"os"
	"path/filepath"
)

func getDockerDesktopPaths() (DockerDesktopPaths, error) {
	_, err := os.Stat("/run/host-services/backend.sock")
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return DockerDesktopPaths{}, err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return DockerDesktopPaths{}, err
		}

		// On Linux
		return DockerDesktopPaths{
			BackendSocket: filepath.Join(home, ".docker", "desktop", "backend.sock"),
		}, nil
	}

	// Inside LinuxKit
	return DockerDesktopPaths{
		BackendSocket: "/run/host-services/backend.sock",
	}, nil
}
