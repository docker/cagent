//go:build !no_docker_desktop && !windows
// +build !no_docker_desktop,!windows

package desktop

import (
	"context"
	"net"
)

func dialBackend(ctx context.Context) (net.Conn, error) {
	return dial(ctx, Paths().BackendSocket)
}

func dial(ctx context.Context, path string) (net.Conn, error) {
	dialer := net.Dialer{}
	return dialer.DialContext(ctx, "unix", path)
}
