//go:build !darwin && !linux && !windows

package desktop

import (
	"errors"

	"github.com/docker/secrets-engine/client/dockerhub"
)

// newSecretsEngineHubAuth reports that the secrets engine is unreachable: the
// SDK only knows where Docker Desktop serves it on macOS, Linux and Windows,
// and under js/wasm there is no socket to dial anyway.
func newSecretsEngineHubAuth() (dockerhub.ClientAuth, error) {
	return nil, errors.New("the secrets engine is not available on this platform")
}
