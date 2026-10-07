//go:build !darwin && !linux && !windows

package desktop

import (
	"errors"

	"github.com/docker/secrets-engine/client/dockerhub"
)

// newSecretsEngineHubAuth always fails: the SDK doesn't support this platform.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	return nil, errors.New("the secrets engine is not available on this platform")
}
