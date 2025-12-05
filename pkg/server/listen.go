package server

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	// Check if socket file exists
	if _, err := os.Stat(path); err == nil {
		// Socket file exists - check if another process is using it
		conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
		if err == nil {
			// Connection succeeded - socket is in use by another process
			conn.Close()
			return nil, fmt.Errorf("socket %s is already in use by another process", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("failed to remove stale socket %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check socket %s: %w", path, err)
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
	return lc.Listen(ctx, "tcp", addr)
}
