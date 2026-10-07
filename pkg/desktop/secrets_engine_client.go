//go:build darwin || linux || windows

package desktop

import (
	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/api"
)

// newSecretsEngineHubAuth connects to the secrets engine Docker Desktop serves
// on its engine socket. The SDK defaults to the standalone daemon's socket,
// which Docker Desktop does not use. Creating the client dials nothing: an
// absent engine only shows up, as an error, on the first lookup.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	engine, err := secretsengine.New(secretsengine.WithSocketPath(api.DesktopSocketPath()))
	if err != nil {
		return nil, err
	}
	return engine.HubAuth(), nil
}
