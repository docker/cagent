//go:build no_docker_desktop

package desktop

import (
	"errors"
	"sync"
)

type DockerDesktopPaths struct {
	BackendSocket string
}

var Paths = sync.OnceValue(func() DockerDesktopPaths {
	// Return empty paths when Docker Desktop is not available
	return DockerDesktopPaths{
		BackendSocket: "",
	}
})

func getDockerDesktopPaths() (DockerDesktopPaths, error) {
	return DockerDesktopPaths{}, errors.New("Docker Desktop is not available (built with no_docker_desktop tag)")
}
