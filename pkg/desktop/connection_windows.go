//go:build !no_docker_desktop && windows
// +build !no_docker_desktop,windows

package desktop

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialBackend(ctx context.Context) (net.Conn, error) {
	return dial(ctx, Paths().BackendSocket)
}

func dial(ctx context.Context, path string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, path)
}
