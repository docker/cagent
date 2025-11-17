package oci

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configv2 "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/version"
)

func TestPackageFileAsOCIToStore(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.yaml")
	testContent := `name: test-app
version: v1.0.0
description: "Test application"
`
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))
	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	tag := "test-app:v1.0.0"
	digest, err := PackageFileAsOCIToStore(testFile, tag, store)
	require.NoError(t, err)

	assert.NotEmpty(t, digest)

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})

	img, err := store.GetArtifactImage(tag)
	require.NoError(t, err)

	assert.NotNil(t, img)

	metadata, err := store.GetArtifactMetadata(tag)
	require.NoError(t, err)

	assert.Equal(t, tag, metadata.Reference)
	assert.Equal(t, digest, metadata.Digest)
}

func TestPackageFileAsOCIToStoreMissingFile(t *testing.T) {
	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)
	_, err = PackageFileAsOCIToStore("/non/existent/file.txt", "test:latest", store)
	require.Error(t, err)
}

func TestPackageFileAsOCIToStoreInvalidTag(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)
	_, err = PackageFileAsOCIToStore(testFile, "", store)
	require.Error(t, err)
}

func TestPackageFileAsOCIToStoreDifferentFileTypes(t *testing.T) {
	testCases := []struct {
		name     string
		filename string
		content  string
		tag      string
	}{
		{
			name:     "yaml file",
			filename: "config.yaml",
			content:  "key: value\nother: data",
			tag:      "config:yaml",
		},
		{
			name:     "json file",
			filename: "data.json",
			content:  `{"key": "value", "number": 42}`,
			tag:      "data:json",
		},
		{
			name:     "text file",
			filename: "readme.txt",
			content:  "This is a simple text file\nwith multiple lines",
			tag:      "readme:txt",
		},
	}

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	var digests []string

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testFile := filepath.Join(t.TempDir(), tc.filename)
			require.NoError(t, os.WriteFile(testFile, []byte(tc.content), 0o644))

			// Package the file as OCI artifact
			digest, err := PackageFileAsOCIToStore(testFile, tc.tag, store)
			require.NoError(t, err)

			digests = append(digests, digest)

			img, err := store.GetArtifactImage(tc.tag)
			require.NoError(t, err)
			assert.NotNil(t, img)
		})
	}

	t.Cleanup(func() {
		for _, digest := range digests {
			if err := store.DeleteArtifact(digest); err != nil {
				t.Logf("Failed to clean up artifact %s: %v", digest, err)
			}
		}
	})
}

func TestPackageFileAsOCIToStoreWithVersion(t *testing.T) {
	t.Parallel()

	// Create test YAML content
	testContent := `version: "2"
metadata:
  author: "Test Author"
  readme: "Test agent"
agents:
  test:
    model: "gpt-4"
    description: "Test agent"
`

	testFile := filepath.Join(t.TempDir(), "test-agent.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	// Create version info
	versionInfo := version.Info{
		Version:   "v1.0.0",
		CreatedAt: "2024-11-17T10:00:00Z",
		Source:    version.SourceCounter,
	}

	tag := "test-agent:v1.0.0"
	digest, err := PackageFileAsOCIToStoreWithVersion(testFile, tag, store, versionInfo)
	require.NoError(t, err)
	assert.NotEmpty(t, digest)

	// Verify the artifact was stored
	img, err := store.GetArtifactImage(tag)
	require.NoError(t, err)
	assert.NotNil(t, img)

	// Note: Annotations are set on the image, but the specific location
	// (manifest vs config) depends on the OCI implementation details
	// The important test is that the YAML content contains the injected metadata

	// Extract and verify the YAML content
	layers, err := img.Layers()
	require.NoError(t, err)
	require.Len(t, layers, 1)

	layerReader, err := layers[0].Uncompressed()
	require.NoError(t, err)
	defer layerReader.Close()

	var yamlContent strings.Builder
	_, err = io.Copy(&yamlContent, layerReader)
	require.NoError(t, err)

	// Parse the modified YAML
	var config configv2.Config
	err = yaml.Unmarshal([]byte(yamlContent.String()), &config)
	require.NoError(t, err)

	// Verify version metadata was injected
	assert.Equal(t, "v1.0.0", config.Metadata.Version)
	assert.Equal(t, "2024-11-17T10:00:00Z", config.Metadata.CreatedAt)
	assert.Equal(t, "Test Author", config.Metadata.Author)
	assert.Equal(t, "Test agent", config.Metadata.Readme)

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})
}

func TestPackageFileAsOCIToStoreWithVersion_ExistingMetadata(t *testing.T) {
	t.Parallel()

	// Create test YAML with existing version metadata
	testContent := `version: "2"
metadata:
  author: "Test Author"
  readme: "Test agent"
  version: "v0.5.0"
  created_at: "2024-01-01T00:00:00Z"
agents:
  test:
    model: "gpt-4"
`

	testFile := filepath.Join(t.TempDir(), "test-agent.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	versionInfo := version.Info{
		Version:   "v2.0.0",
		CreatedAt: "2024-11-17T10:00:00Z",
		Source:    version.SourceExplicit,
	}

	tag := "test-agent:v2.0.0"
	digest, err := PackageFileAsOCIToStoreWithVersion(testFile, tag, store, versionInfo)
	require.NoError(t, err)

	// Extract and verify the YAML content
	img, err := store.GetArtifactImage(tag)
	require.NoError(t, err)

	layers, err := img.Layers()
	require.NoError(t, err)
	layerReader, err := layers[0].Uncompressed()
	require.NoError(t, err)
	defer layerReader.Close()

	var yamlContent strings.Builder
	_, err = io.Copy(&yamlContent, layerReader)
	require.NoError(t, err)

	var config configv2.Config
	err = yaml.Unmarshal([]byte(yamlContent.String()), &config)
	require.NoError(t, err)

	// Verify existing metadata was preserved (not overwritten)
	assert.Equal(t, "v0.5.0", config.Metadata.Version)
	assert.Equal(t, "2024-01-01T00:00:00Z", config.Metadata.CreatedAt)

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})
}

func TestPackageFileAsOCIToStoreWithVersion_EmptyMetadata(t *testing.T) {
	t.Parallel()

	// Create test YAML with no metadata section
	testContent := `version: "2"
agents:
  test:
    model: "gpt-4"
    description: "Test agent"
`

	testFile := filepath.Join(t.TempDir(), "test-agent.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	versionInfo := version.Info{
		Version:   "v1.5.0",
		CreatedAt: "2024-11-17T15:30:00Z",
		Source:    version.SourceCounter,
	}

	tag := "test-agent:v1.5.0"
	digest, err := PackageFileAsOCIToStoreWithVersion(testFile, tag, store, versionInfo)
	require.NoError(t, err)

	// Extract and verify the YAML content
	img, err := store.GetArtifactImage(tag)
	require.NoError(t, err)

	layers, err := img.Layers()
	require.NoError(t, err)
	layerReader, err := layers[0].Uncompressed()
	require.NoError(t, err)
	defer layerReader.Close()

	var yamlContent strings.Builder
	_, err = io.Copy(&yamlContent, layerReader)
	require.NoError(t, err)

	var config configv2.Config
	err = yaml.Unmarshal([]byte(yamlContent.String()), &config)
	require.NoError(t, err)

	// Verify version metadata was injected into empty metadata
	assert.Equal(t, "v1.5.0", config.Metadata.Version)
	assert.Equal(t, "2024-11-17T15:30:00Z", config.Metadata.CreatedAt)

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})
}

func TestPackageFileAsOCIToStoreWithVersion_InvalidYAML(t *testing.T) {
	t.Parallel()

	// Create invalid YAML content
	testContent := `version: "2"
metadata:
  author: "Test Author"
  readme: [unclosed bracket
agents:
  test:
    model: "gpt-4"
`

	testFile := filepath.Join(t.TempDir(), "invalid.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	versionInfo := version.Info{
		Version:   "v1.0.0",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    version.SourceCounter,
	}

	tag := "invalid:v1.0.0"
	_, err = PackageFileAsOCIToStoreWithVersion(testFile, tag, store, versionInfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing YAML")
}

func TestPackageFileAsOCIToStoreWithVersion_MissingFile(t *testing.T) {
	t.Parallel()

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)

	versionInfo := version.Info{
		Version:   "v1.0.0",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Source:    version.SourceCounter,
	}

	_, err = PackageFileAsOCIToStoreWithVersion("/nonexistent/file.yaml", "test:v1.0.0", store, versionInfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading file")
}
