package oci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/cagent/pkg/content"
)

func TestPackageFileAsOCIToStore(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.yaml")
	testContent := `name: test-app
version: v1.0.0
description: "Test application"
`
	if err := os.WriteFile(testFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}
	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create content store: %v", err)
	}

	tag := "test-app:v1.0.0"
	digest, err := PackageFileAsOCIToStore(testFile, tag, store)
	if err != nil {
		t.Fatalf("Failed to package file as OCI: %v", err)
	}

	if digest == "" {
		t.Fatal("Digest is empty")
	}

	t.Cleanup(func() {
		if err := store.DeleteArtifact(digest); err != nil {
			t.Logf("Failed to clean up artifact: %v", err)
		}
	})

	img, err := store.GetArtifactImage(tag)
	if err != nil {
		t.Fatalf("Failed to retrieve artifact: %v", err)
	}

	if img == nil {
		t.Fatal("Retrieved image is nil")
	}

	metadata, err := store.GetArtifactMetadata(tag)
	if err != nil {
		t.Fatalf("Failed to get metadata: %v", err)
	}

	if metadata.Reference != tag {
		t.Errorf("Expected reference %s, got %s", tag, metadata.Reference)
	}

	if metadata.Digest != digest {
		t.Errorf("Expected digest %s, got %s", digest, metadata.Digest)
	}
}

func TestPackageFileAsOCIToStoreMissingFile(t *testing.T) {
	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create content store: %v", err)
	}
	_, err = PackageFileAsOCIToStore("/non/existent/file.txt", "test:latest", store)
	if err == nil {
		t.Fatal("Expected error for missing file")
	}
}

func TestPackageFileAsOCIToStoreInvalidTag(t *testing.T) {
	testFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0o644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	store, err := content.NewStore(content.WithBaseDir(t.TempDir()))
	if err != nil {
		t.Fatalf("Failed to create content store: %v", err)
	}
	_, err = PackageFileAsOCIToStore(testFile, "", store)
	if err == nil {
		t.Fatal("Expected error for empty tag")
	}
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
	if err != nil {
		t.Fatalf("Failed to create content store: %v", err)
	}

	var digests []string

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create test file
			testFile := filepath.Join(t.TempDir(), tc.filename)
			if err := os.WriteFile(testFile, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Package the file as OCI artifact
			digest, err := PackageFileAsOCIToStore(testFile, tc.tag, store)
			if err != nil {
				t.Fatalf("Failed to package file as OCI: %v", err)
			}

			digests = append(digests, digest)

			// Verify the artifact was stored
			img, err := store.GetArtifactImage(tc.tag)
			if err != nil {
				t.Errorf("Failed to retrieve artifact %s: %v", tc.tag, err)
			}

			if img == nil {
				t.Errorf("Retrieved image for %s is nil", tc.tag)
			}
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
