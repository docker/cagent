//go:build !darwin && !linux && !windows

package desktop

import (
	"errors"

	"github.com/docker/secrets-engine/client/dockerhub"
)

// errNoSecretsEngine means the SDK doesn't support this platform.
var errNoSecretsEngine = errors.New("the secrets engine is not available on this platform")

// newSecretsEngineHubAuth always fails with errNoSecretsEngine.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	return nil, errNoSecretsEngine
}
