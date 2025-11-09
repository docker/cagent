package root

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/agentfile"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/oci"
)

func TestOciRefToFilename(t *testing.T) {
	tests := []struct {
		name     string
		ociRef   string
		expected string
	}{
		{
			name:     "simple reference",
			ociRef:   "myagent",
			expected: "myagent.yaml",
		},
		{
			name:     "reference with registry and tag",
			ociRef:   "docker.io/myorg/agent:v1",
			expected: "docker.io_myorg_agent_v1.yaml",
		},
		{
			name:     "localhost with port",
			ociRef:   "localhost:5000/test",
			expected: "localhost_5000_test.yaml",
		},
		{
			name:     "reference with digest",
			ociRef:   "myregistry.io/org/app@sha256:abc123",
			expected: "myregistry.io_org_app_sha256_abc123.yaml",
		},
		{
			name:     "already has .yaml extension",
			ociRef:   "myagent.yaml",
			expected: "myagent.yaml",
		},
		{
			name:     "complex path",
			ociRef:   "registry.example.com:443/project/subproject/agent:latest",
			expected: "registry.example.com_443_project_subproject_agent_latest.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := agentfile.OciRefToFilename(tt.ociRef)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveAgentFile_LocalFile(t *testing.T) {
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "test-agent.yaml")
	yamlContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test agent
    instruction: You are a test agent
`
	require.NoError(t, os.WriteFile(yamlFile, []byte(yamlContent), 0o644))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	resolved, err := agentfile.Resolve(ctx, nil, yamlFile)
	require.NoError(t, err)

	// Should return absolute path
	absPath, err := filepath.Abs(yamlFile)
	require.NoError(t, err)
	assert.Equal(t, absPath, resolved)
}

func TestResolveAgentFile_OCIRef_ConsistentFilename(t *testing.T) {
	storeDir := t.TempDir()
	t.Setenv("CAGENT_CONTENT_STORE", storeDir)

	store, err := content.NewStore()
	require.NoError(t, err)

	agentContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test OCI agent
    instruction: You are a test OCI agent
`
	agentFile := filepath.Join(t.TempDir(), "oci-agent.yaml")
	require.NoError(t, os.WriteFile(agentFile, []byte(agentContent), 0o644))

	// Package as OCI artifact
	ociRef := "test.registry.io/myorg/testagent:v1"
	_, err = oci.PackageFileAsOCIToStore(agentFile, ociRef, store)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// First resolution
	resolved1, err := agentfile.Resolve(ctx, nil, ociRef)
	require.NoError(t, err)
	assert.NotEmpty(t, resolved1)

	content1, err := os.ReadFile(resolved1)
	require.NoError(t, err)
	assert.Equal(t, agentContent, string(content1))

	// Expected filename based on OCI ref
	expectedFilename := agentfile.OciRefToFilename(ociRef)
	assert.Equal(t, expectedFilename, filepath.Base(resolved1))

	// Store the first resolved path
	firstResolvedPath := resolved1

	// Second resolution (simulating a reload)
	resolved2, err := agentfile.Resolve(ctx, nil, ociRef)
	require.NoError(t, err)

	// Should return the SAME filename
	assert.Equal(t, resolved1, resolved2, "Subsequent resolutions should return the same file path")
	assert.Equal(t, filepath.Base(resolved1), filepath.Base(resolved2), "Filenames should be identical")

	content2, err := os.ReadFile(resolved2)
	require.NoError(t, err)
	assert.Equal(t, agentContent, string(content2))

	// Update the agent content in the OCI store
	updatedContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o-mini
    description: Updated test OCI agent
    instruction: You are an updated test OCI agent
`
	updatedFile := filepath.Join(t.TempDir(), "updated-agent.yaml")
	require.NoError(t, os.WriteFile(updatedFile, []byte(updatedContent), 0o644))
	_, err = oci.PackageFileAsOCIToStore(updatedFile, ociRef, store)
	require.NoError(t, err)

	// Third resolution (simulating reload after update)
	resolved3, err := agentfile.Resolve(ctx, nil, ociRef)
	require.NoError(t, err)

	// Should STILL use the same filename
	assert.Equal(t, firstResolvedPath, resolved3, "Even after OCI update, should use same file path")

	// But content should be updated
	content3, err := os.ReadFile(resolved3)
	require.NoError(t, err)
	assert.Equal(t, updatedContent, string(content3), "Content should be updated from OCI store")
}

func TestResolveAgentFile_MultipleOCIRefs_DifferentFilenames(t *testing.T) {
	storeDir := t.TempDir()
	t.Setenv("CAGENT_CONTENT_STORE", storeDir)

	store, err := content.NewStore()
	require.NoError(t, err)

	agent1Content := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Agent 1
    instruction: You are agent 1
`
	agent2Content := `version: "1"
agents:
  root:
    model: anthropic/claude-sonnet-4-0
    description: Agent 2
    instruction: You are agent 2
`

	agent1File := filepath.Join(t.TempDir(), "agent1.yaml")
	agent2File := filepath.Join(t.TempDir(), "agent2.yaml")
	require.NoError(t, os.WriteFile(agent1File, []byte(agent1Content), 0o644))
	require.NoError(t, os.WriteFile(agent2File, []byte(agent2Content), 0o644))

	// Package as different OCI artifacts
	ociRef1 := "test.io/org/agent1:v1"
	ociRef2 := "test.io/org/agent2:v1"
	_, err = oci.PackageFileAsOCIToStore(agent1File, ociRef1, store)
	require.NoError(t, err)
	_, err = oci.PackageFileAsOCIToStore(agent2File, ociRef2, store)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Resolve both OCI refs
	resolved1, err := agentfile.Resolve(ctx, nil, ociRef1)
	require.NoError(t, err)

	resolved2, err := agentfile.Resolve(ctx, nil, ociRef2)
	require.NoError(t, err)

	// Should have DIFFERENT filenames
	assert.NotEqual(t, resolved1, resolved2, "Different OCI refs should produce different file paths")
	assert.NotEqual(t, filepath.Base(resolved1), filepath.Base(resolved2), "Different OCI refs should produce different filenames")

	content1, err := os.ReadFile(resolved1)
	require.NoError(t, err)
	assert.Equal(t, agent1Content, string(content1))

	content2, err := os.ReadFile(resolved2)
	require.NoError(t, err)
	assert.Equal(t, agent2Content, string(content2))

	assert.Equal(t, agentfile.OciRefToFilename(ociRef1), filepath.Base(resolved1))
	assert.Equal(t, agentfile.OciRefToFilename(ociRef2), filepath.Base(resolved2))
}

func TestResolveAgentFile_ContextCancellation(t *testing.T) {
	storeDir := t.TempDir()
	t.Setenv("CAGENT_CONTENT_STORE", storeDir)

	store, err := content.NewStore()
	require.NoError(t, err)

	agentContent := `version: "1"
agents:
  root:
    model: openai/gpt-4o
    description: Test agent
    instruction: You are a test agent
`
	agentFile := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(agentFile, []byte(agentContent), 0o644))

	// Package as OCI artifact
	ociRef := "test.io/cleanup/agent:v1"
	_, err = oci.PackageFileAsOCIToStore(agentFile, ociRef, store)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	// Resolve the OCI ref
	resolved, err := agentfile.Resolve(ctx, nil, ociRef)
	require.NoError(t, err)
	assert.FileExists(t, resolved)

	// Cancel the context to trigger cleanup
	cancel()

	// Give the cleanup goroutine time to execute
	time.Sleep(100 * time.Millisecond)

	// File should be deleted
	_, err = os.Stat(resolved)
	assert.True(t, os.IsNotExist(err), "File should be cleaned up after context cancellation")
}
