//go:build darwin || linux || windows

package desktop

import (
	"errors"

	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/api"

	"github.com/docker/docker-agent/pkg/hubauth"
)

// errNoSecretsEngine means nothing is listening on the engine socket.
var errNoSecretsEngine = secretsengine.ErrSecretsEngineNotRunning

// newSecretsEngineHubAuth connects to Docker Desktop's secrets engine, reading staging sessions on staging.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	engine, err := secretsengine.New(secretsengine.WithSocketPath(api.DesktopSocketPath()))
	if err != nil {
		return nil, err
	}
	if hubauth.Staging() {
		return engine.HubAuth(dockerhub.Staging()), nil
	}
	return engine.HubAuth(), nil
}

// secretsEngineHint returns the SDK's recovery hint for a failed connection, or "".
func secretsEngineHint(err error) string {
	if connErr, ok := errors.AsType[*secretsengine.ConnectError](err); ok {
		return connErr.Hint()
	}
	return ""
}
