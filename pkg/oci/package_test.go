package oci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/content"
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
	assert.Error(t, err)
}

func TestPackageFileAsOCIToStoreInvalidTag(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0o644))

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	require.NoError(t, err)
	_, err = PackageFileAsOCIToStore(testFile, "", store)
	assert.Error(t, err)
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
			// Create test file
			testFile := filepath.Join(t.TempDir(), tc.filename)
			require.NoError(t, os.WriteFile(testFile, []byte(tc.content), 0o644))

			// Package the file as OCI artifact
			digest, err := PackageFileAsOCIToStore(testFile, tc.tag, store)
			require.NoError(t, err)

			digests = append(digests, digest)

			// Verify the artifact was stored
			img, err := store.GetArtifactImage(tc.tag)
			assert.NoError(t, err)
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
