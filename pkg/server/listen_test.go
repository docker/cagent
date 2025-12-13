//go:build !windows

package server

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListenUnix_FailsWhenSocketInUse(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "test.sock")

	// Start a listener on the socket
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	defer listener.Close()

	// Try to listen on the same socket - should fail fast
	_, err = Listen(t.Context(), "unix://"+socketPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in use")
}

func TestListenUnix_SucceedsWithStaleSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "stale.sock")

	// Create a listener and close it immediately to leave a stale socket file
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	listener.Close()

	// The socket file should still exist but be stale
	// Listen should succeed by removing the stale socket
	newListener, err := Listen(t.Context(), "unix://"+socketPath)
	require.NoError(t, err)
	defer newListener.Close()

	assert.NotNil(t, newListener)
}

func TestListenUnix_SucceedsWithNoExistingSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "new.sock")

	// Listen on a new socket path - should succeed
	listener, err := Listen(t.Context(), "unix://"+socketPath)
	require.NoError(t, err)
	defer listener.Close()

	assert.NotNil(t, listener)
}

func TestListenTCP_FailsWhenPortInUse(t *testing.T) {
	t.Parallel()

	// Start a listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	// Get the port that was allocated
	addr := listener.Addr().String()

	// Try to listen on the same address - should fail
	_, err = Listen(t.Context(), addr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address already in use")
}
