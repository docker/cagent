//go:build no_docker_desktop

package desktop

import (
	"context"
	"testing"
)

func TestGetTokenWithoutDockerDesktop(t *testing.T) {
	ctx := context.Background()
	token := GetToken(ctx)
	
	// When built with no_docker_desktop tag, GetToken should return empty string
	// (unless DOCKER_TOKEN env var is set)
	if token != "" && token != "test-token" {
		t.Errorf("Expected empty token or test-token, got: %s", token)
	}
}

func TestPathsWithoutDockerDesktop(t *testing.T) {
	paths := Paths()
	
	// When built with no_docker_desktop tag, BackendSocket should be empty
	if paths.BackendSocket != "" {
		t.Errorf("Expected empty BackendSocket, got: %s", paths.BackendSocket)
	}
}

func TestClientBackendWithoutDockerDesktop(t *testing.T) {
	ctx := context.Background()
	var result string
	
	err := ClientBackend.Get(ctx, "/test", &result)
	
	// When built with no_docker_desktop tag, should return error
	if err == nil {
		t.Error("Expected error from ClientBackend.Get, but got nil")
	}
	
	expectedErrMsg := "Docker Desktop is not available (built with no_docker_desktop tag)"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message '%s', got: %s", expectedErrMsg, err.Error())
	}
}