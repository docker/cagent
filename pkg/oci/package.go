package oci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	"github.com/docker/cagent/pkg/config"
	"github.com/docker/cagent/pkg/content"
)

// PackageFileAsOCIToStore creates an OCI artifact from a file and stores it in the content store
func PackageFileAsOCIToStore(filePath, artifactRef string, store *content.Store) (string, error) {
	if !strings.Contains(artifactRef, ":") {
		artifactRef += ":latest"
	}

	// Validate the file path to prevent directory traversal attacks
	validatedPath, err := config.ValidatePathInDirectory(filePath, "")
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	data, err := os.ReadFile(validatedPath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	layer := static.NewLayer(data, types.OCIUncompressedLayer)

	img := empty.Image

	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return "", fmt.Errorf("appending layer: %w", err)
	}

	annotations := map[string]string{
		"org.opencontainers.image.created":     time.Now().Format(time.RFC3339),
		"org.opencontainers.image.description": fmt.Sprintf("OCI artifact containing %s", filepath.Base(validatedPath)),
	}

	img = mutate.Annotations(img, annotations).(v1.Image)

	digest, err := store.StoreArtifact(img, artifactRef)
	if err != nil {
		return "", fmt.Errorf("storing artifact in content store: %w", err)
	}

	return digest, nil
}
