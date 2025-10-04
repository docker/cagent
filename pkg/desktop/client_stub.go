//go:build no_docker_desktop

package desktop

import (
	"context"
	"errors"
	"net"
)

// Stub client when Docker Desktop is not available
var ClientBackend = newStubClient()

type RawClient struct {
	// Empty implementation
}

func newStubClient() *RawClient {
	return &RawClient{}
}

func (c *RawClient) Get(ctx context.Context, endpoint string, v any) error {
	return errors.New("Docker Desktop is not available (built with no_docker_desktop tag)")
}

func dialBackend(ctx context.Context) (net.Conn, error) {
	return nil, errors.New("Docker Desktop is not available (built with no_docker_desktop tag)")
}

func dial(ctx context.Context, path string) (net.Conn, error) {
	return nil, errors.New("Docker Desktop is not available (built with no_docker_desktop tag)")
}