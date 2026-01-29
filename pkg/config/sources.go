package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/remote"
)

type Source interface {
	Name() string
	ParentDir() string
	Read(ctx context.Context) ([]byte, error)
}

type Sources map[string]Source

// fileSource is used to load an agent configuration from a YAML file.
type fileSource struct {
	path string
}

func NewFileSource(path string) Source {
	return fileSource{
		path: path,
	}
}

func (a fileSource) Name() string {
	return a.path
}

func (a fileSource) ParentDir() string {
	return filepath.Dir(a.path)
}

func (a fileSource) Read(context.Context) ([]byte, error) {
	parentDir := a.ParentDir()
	fs, err := os.OpenRoot(parentDir)
	if err != nil {
		return nil, fmt.Errorf("opening filesystem %s: %w", parentDir, err)
	}

	fileName := filepath.Base(a.path)
	data, err := fs.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", fileName, err)
	}

	return data, nil
}

// bytesSource is used to load an agent configuration from a []byte.
type bytesSource struct {
	name string
	data []byte
}

func NewBytesSource(name string, data []byte) Source {
	return bytesSource{
		name: name,
		data: data,
	}
}

func (a bytesSource) Name() string {
	return a.name
}

func (a bytesSource) ParentDir() string {
	return ""
}

func (a bytesSource) Read(context.Context) ([]byte, error) {
	return a.data, nil
}

// ociSource is used to load an agent configuration from an OCI artifact.
type ociSource struct {
	reference string
}

func NewOCISource(reference string) Source {
	return ociSource{
		reference: reference,
	}
}

func (a ociSource) Name() string {
	return a.reference
}

func (a ociSource) ParentDir() string {
	return ""
}

// Read loads an agent configuration from an OCI artifact
//
// The OCI registry remains the source of truth
// The local content store is used as a cache and fallback only
// A forced re-pull is triggered exclusively when store corruption is detected
func (a ociSource) Read(ctx context.Context) ([]byte, error) {
	store, err := content.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to create content store: %w", err)
	}

	tryLoad := func() ([]byte, error) {
		af, err := store.GetArtifact(a.reference)
		if err != nil {
			return nil, err
		}
		return []byte(af), nil
	}

	// Check if we have any local metadata (same as before)
	_, metaErr := store.GetArtifactMetadata(a.reference)
	hasLocal := metaErr == nil

	// Always try normal pull first (preserves pull-interval behavior)
	if _, pullErr := remote.Pull(ctx, a.reference, false); pullErr != nil {
		if !hasLocal {
			return nil, fmt.Errorf("failed to pull OCI image %s: %w", a.reference, pullErr)
		}

		slog.Debug(
			"Failed to check for OCI reference updates, using cached version",
			"ref", a.reference,
			"error", pullErr,
		)
	}

	// Try loading from store
	data, err := tryLoad()
	if err == nil {
		return data, nil
	}

	// If loading failed due to corruption, force re-pull
	if errors.Is(err, content.ErrStoreCorrupted) {
		slog.Warn(
			"Local OCI store corrupted, forcing re-pull",
			"ref", a.reference,
		)

		if _, pullErr := remote.Pull(ctx, a.reference, true); pullErr != nil {
			return nil, fmt.Errorf("failed to force re-pull OCI image %s: %w", a.reference, pullErr)
		}

		data, err = tryLoad()
		if err == nil {
			return data, nil
		}
	}

	return nil, fmt.Errorf(
		"failed to load agent from OCI source %s: %w",
		a.reference,
		err,
	)
}

// urlSource is used to load an agent configuration from an HTTP/HTTPS URL.
type urlSource struct {
	url string
}

func NewURLSource(url string) Source {
	return urlSource{
		url: url,
	}
}

func (a urlSource) Name() string {
	return a.url
}

func (a urlSource) ParentDir() string {
	return ""
}

// getURLCacheDir returns the directory used to cache URL-based agent configurations.
func getURLCacheDir() string {
	return filepath.Join(paths.GetDataDir(), "url_cache")
}

func (a urlSource) Read(ctx context.Context) ([]byte, error) {
	cacheDir := getURLCacheDir()
	urlHash := hashURL(a.url)
	cachePath := filepath.Join(cacheDir, urlHash)
	etagPath := cachePath + ".etag"

	// Read cached ETag if available
	cachedETag := ""
	if etagData, err := os.ReadFile(etagPath); err == nil {
		cachedETag = string(etagData)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Include If-None-Match header if we have a cached ETag
	if cachedETag != "" {
		req.Header.Set("If-None-Match", cachedETag)
	}

	resp, err := httpclient.NewHTTPClient().Do(req)
	if err != nil {
		// Network error - try to use cached version
		if cachedData, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			slog.Debug("Network error fetching URL, using cached version", "url", a.url, "error", err)
			return cachedData, nil
		}
		return nil, fmt.Errorf("fetching %s: %w", a.url, err)
	}
	defer resp.Body.Close()

	// 304 Not Modified - return cached content
	if resp.StatusCode == http.StatusNotModified {
		if cachedData, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			slog.Debug("URL not modified, using cached version", "url", a.url)
			return cachedData, nil
		}
		// Cache file missing despite 304, fall through to fetch again
	}

	if resp.StatusCode != http.StatusOK {
		// HTTP error - try to use cached version
		if cachedData, cacheErr := os.ReadFile(cachePath); cacheErr == nil {
			slog.Debug("HTTP error fetching URL, using cached version", "url", a.url, "status", resp.Status)
			return cachedData, nil
		}
		return nil, fmt.Errorf("fetching %s: %s", a.url, resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	// Cache the response
	if err := os.MkdirAll(cacheDir, 0o755); err == nil {
		if err := os.WriteFile(cachePath, data, 0o644); err != nil {
			slog.Debug("Failed to cache URL content", "url", a.url, "error", err)
		}

		// Save ETag if present
		if etag := resp.Header.Get("ETag"); etag != "" {
			if err := os.WriteFile(etagPath, []byte(etag), 0o644); err != nil {
				slog.Debug("Failed to cache ETag", "url", a.url, "error", err)
			}
		} else {
			// Remove stale ETag file if server no longer provides ETag
			_ = os.Remove(etagPath)
		}
	}

	return data, nil
}

// hashURL creates a safe filename from a URL.
func hashURL(url string) string {
	h := sha256.Sum256([]byte(url))
	return hex.EncodeToString(h[:])
}

// IsURLReference checks if the input is a valid HTTP/HTTPS URL.
func IsURLReference(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}
