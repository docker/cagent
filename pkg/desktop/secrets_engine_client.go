//go:build darwin || linux || windows

package desktop

import (
	"errors"

	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/api"
)

// errNoSecretsEngine means nothing is listening on the engine socket.
var errNoSecretsEngine = secretsengine.ErrSecretsEngineNotRunning

// newSecretsEngineHubAuth connects to Docker Desktop's secrets engine socket.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	engine, err := secretsengine.New(secretsengine.WithSocketPath(api.DesktopSocketPath()))
	if err != nil {
		return nil, err
	}
	return engine.HubAuth(secretsEngineHubOptions()...), nil
}

// secretsEngineHint returns the SDK's recovery hint for a failed connection, or "".
func secretsEngineHint(err error) string {
	if connErr, ok := errors.AsType[*secretsengine.ConnectError](err); ok {
		return connErr.Hint()
	}
	return ""
}
