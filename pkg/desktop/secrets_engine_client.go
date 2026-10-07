//go:build darwin || linux || windows

package desktop

import (
	"errors"

	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/api"
)

// newSecretsEngineHubAuth connects to Docker Desktop's engine socket, not the
// SDK's default standalone daemon socket.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	engine, err := secretsengine.New(secretsengine.WithSocketPath(api.DesktopSocketPath()))
	if err != nil {
		return nil, err
	}
	return engine.HubAuth(), nil
}

// secretsEngineUnavailable reports whether err means nothing is listening on
// the engine socket: Docker Desktop is not installed, not running, or predates
// the engine.
func secretsEngineUnavailable(err error) bool {
	return errors.Is(err, secretsengine.ErrSecretsEngineNotAvailable)
}
