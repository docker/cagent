package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
)

func Listen(ctx context.Context, addr string) (net.Listener, error) {
	if path, ok := strings.CutPrefix(addr, "unix://"); ok {
		return listenUnix(ctx, path)
	}

	if path, ok := strings.CutPrefix(addr, "npipe://"); ok {
		return listenNamedPipe(path)
	}

	return listenTCP(ctx, addr)
}

func listenUnix(ctx context.Context, path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	var lnConfig net.ListenConfig
	return lnConfig.Listen(ctx, "unix", path)
}

func listenTCP(ctx context.Context, addr string) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(ctx, "tcp4", addr)
}
