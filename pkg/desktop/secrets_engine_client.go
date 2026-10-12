//go:build darwin || linux || windows

package desktop

import (
	"errors"

	secretsengine "github.com/docker/secrets-engine/client"
	"github.com/docker/secrets-engine/client/dockerhub"
	"github.com/docker/secrets-engine/x/api"
)

// errNoSecretsEngine means nothing is at the engine socket: Docker Desktop is
// not installed, not running, or predates the engine.
var errNoSecretsEngine = secretsengine.ErrSecretsEngineNotRunning

// newSecretsEngineHubAuth connects to Docker Desktop's engine socket, not the
// SDK's default standalone daemon socket.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	engine, err := secretsengine.New(secretsengine.WithSocketPath(api.DesktopSocketPath()))
	if err != nil {
		return nil, err
	}
	return engine.HubAuth(secretsEngineHubOptions()...), nil
}

// secretsEngineHint returns the SDK's advice for recovering from a failed
// connection to the engine, or "" when err is not one.
func secretsEngineHint(err error) string {
	if connErr, ok := errors.AsType[*secretsengine.ConnectError](err); ok {
		return connErr.Hint()
	}
	return ""
}
