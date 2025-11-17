package oci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"

	configv2 "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/path"
	"github.com/docker/cagent/pkg/version"
)

// PackageFileAsOCIToStore creates an OCI artifact from a file and stores it in the content store
func PackageFileAsOCIToStore(filePath, artifactRef string, store *content.Store) (string, error) {
	if !strings.Contains(artifactRef, ":") {
		artifactRef += ":latest"
	}

	// Validate the file path to prevent directory traversal attacks
	validatedPath, err := path.ValidatePathInDirectory(filePath, "")
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

// PackageFileAsOCIToStoreWithVersion creates an OCI artifact with version metadata injection
func PackageFileAsOCIToStoreWithVersion(filePath, artifactRef string, store *content.Store, versionInfo version.Info) (string, error) {
	if !strings.Contains(artifactRef, ":") {
		artifactRef += ":latest"
	}

	// Validate the file path to prevent directory traversal attacks
	validatedPath, err := path.ValidatePathInDirectory(filePath, "")
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}

	// Read and parse the YAML file
	data, err := os.ReadFile(validatedPath)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	// Parse the YAML
	var config configv2.Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parsing YAML: %w", err)
	}

	// Inject version metadata
	if config.Metadata.Version == "" {
		config.Metadata.Version = versionInfo.Version
	}
	if config.Metadata.CreatedAt == "" {
		config.Metadata.CreatedAt = versionInfo.CreatedAt
	}

	// Marshal back to YAML
	modifiedData, err := yaml.Marshal(&config)
	if err != nil {
		return "", fmt.Errorf("marshaling YAML with version metadata: %w", err)
	}

	layer := static.NewLayer(modifiedData, types.OCIUncompressedLayer)

	img := empty.Image

	img, err = mutate.AppendLayers(img, layer)
	if err != nil {
		return "", fmt.Errorf("appending layer: %w", err)
	}

	annotations := map[string]string{
		"org.opencontainers.image.created":     time.Now().Format(time.RFC3339),
		"org.opencontainers.image.description": fmt.Sprintf("OCI artifact containing %s", filepath.Base(validatedPath)),
		"org.opencontainers.image.version":     versionInfo.Version,
	}

	img = mutate.Annotations(img, annotations).(v1.Image)

	digest, err := store.StoreArtifact(img, artifactRef)
	if err != nil {
		return "", fmt.Errorf("storing artifact in content store: %w", err)
	}

	return digest, nil
}
