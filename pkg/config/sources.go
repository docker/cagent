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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/paths"
	"github.com/docker/cagent/pkg/remote"
	"github.com/moby/buildkit/frontend/dockerfile/dfgitutil"
	"github.com/moby/buildkit/util/gitutil"
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
	defer fs.Close()

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
	url         string
	envProvider environment.Provider
}

// NewURLSource creates a new URL source. If envProvider is non-nil, it will be used
// to look up GITHUB_TOKEN for authentication when fetching from GitHub URLs.
func NewURLSource(rawURL string, envProvider environment.Provider) Source {
	return &urlSource{
		url:         rawURL,
		envProvider: envProvider,
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

	// Add GitHub token authorization for GitHub URLs
	a.addGitHubAuth(ctx, req)

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

// githubHosts lists the hostnames that support GitHub token authentication.
var githubHosts = []string{
	"github.com",
	"raw.githubusercontent.com",
	"gist.githubusercontent.com",
}

// isGitHubURL checks if the URL is a GitHub URL that can use token authentication.
// It performs strict hostname validation to prevent token leakage to malicious domains.
func isGitHubURL(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	return slices.Contains(githubHosts, u.Host)
}

// addGitHubAuth adds GitHub token authorization to the request if:
// - The URL is a GitHub URL
// - An environment provider is configured
// - GITHUB_TOKEN is available in the environment
func (a urlSource) addGitHubAuth(ctx context.Context, req *http.Request) {
	if a.envProvider == nil {
		return
	}

	if !isGitHubURL(a.url) {
		return
	}

	token, ok := a.envProvider.Get(ctx, "GITHUB_TOKEN")
	if !ok || token == "" {
		return
	}

	req.Header.Set("Authorization", "Bearer "+token)
	slog.Debug("Added GitHub token authorization to request", "url", a.url)
}

// hashURL creates a safe filename from a URL.
func hashURL(rawURL string) string {
	h := sha256.Sum256([]byte(rawURL))
	return hex.EncodeToString(h[:])
}

// IsURLReference checks if the input is a valid HTTP/HTTPS URL.
func IsURLReference(input string) bool {
	return strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://")
}

// gitSource loads an agent configuration from a git repository.
type gitSource struct {
	gitURL    string
	ref       *dfgitutil.GitRef
	clonePath string
}

// NewGitSource creates a new git source from a git URL.
func NewGitSource(gitURL string) (Source, error) {
	ref, isGit, err := dfgitutil.ParseGitRef(gitURL)
	if err != nil {
		return nil, err
	}
	if !isGit || ref == nil {
		return nil, fmt.Errorf("invalid git URL: %s", gitURL)
	}
	return &gitSource{
		gitURL: gitURL,
		ref:    ref,
	}, nil
}

func (s *gitSource) Name() string {
	return s.gitURL
}

func (s *gitSource) ParentDir() string {
	return s.clonePath
}

func (s *gitSource) Read(ctx context.Context) ([]byte, error) {
	if err := checkGitInstalled(ctx); err != nil {
		return nil, err
	}
	tmpDir, err := os.MkdirTemp("", "cagent-git-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	slog.Debug("Cloning git repository", "remote", s.ref.Remote, "ref", s.ref.Ref, "path", tmpDir)
	if err := cloneRepository(ctx, s.ref, tmpDir); err != nil {
		return nil, err
	}
	finalPath := tmpDir
	if s.ref.SubDir != "" {
		finalPath = filepath.Join(tmpDir, s.ref.SubDir)
		if info, err := os.Stat(finalPath); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("subdirectory not found: %s", s.ref.SubDir)
		}
	}
	configPath, err := findAgentConfig(finalPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading agent configuration: %w", err)
	}
	return data, nil
}

// IsGitReference checks if the input is a git URL reference by testing
// if dfgitutil can parse it as a valid git reference.
func IsGitReference(input string) bool {
	if input == "" {
		return false
	}
	ref, isGit, err := dfgitutil.ParseGitRef(input)
	return err == nil && isGit && ref != nil
}

func checkGitInstalled(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "--version")
	if err := cmd.Run(); err != nil {
		return errors.New("git is not installed or not in PATH")
	}
	return nil
}

func cloneRepository(ctx context.Context, ref *dfgitutil.GitRef, destDir string) error {
	args := []string{"clone", "--depth", "1"}
	if ref.Ref != "" && !gitutil.IsCommitSHA(ref.Ref) {
		args = append(args, "--branch", ref.Ref)
	}
	args = append(args, "--single-branch", ref.Remote, destDir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ref.Ref != "" && strings.Contains(string(output), "not found") {
			return cloneAndCheckout(ctx, ref, destDir)
		}
		return fmt.Errorf("git clone failed: %s", string(output))
	}
	if ref.Ref != "" && gitutil.IsCommitSHA(ref.Ref) {
		return checkoutRef(ctx, destDir, ref.Ref)
	}
	return nil
}

func cloneAndCheckout(ctx context.Context, ref *dfgitutil.GitRef, destDir string) error {
	_ = os.RemoveAll(destDir)
	args := []string{"clone", ref.Remote, destDir}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %s", string(output))
	}
	return checkoutRef(ctx, destDir, ref.Ref)
}

func checkoutRef(ctx context.Context, repoDir, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", ref)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout failed: %s", string(output))
	}
	return nil
}

func findAgentConfig(dir string) (string, error) {
	knownNames := []string{"agent.yaml", "agent.yml", "cagent.yaml", "cagent.yml"}
	for _, name := range knownNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading directory: %w", err)
	}
	var yamlFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext == ".yaml" || ext == ".yml" {
			yamlFiles = append(yamlFiles, filepath.Join(dir, entry.Name()))
		}
	}
	if len(yamlFiles) == 1 {
		return yamlFiles[0], nil
	}
	if len(yamlFiles) > 1 {
		return "", fmt.Errorf("multiple YAML files found in %s, please specify which one to use", dir)
	}
	return "", fmt.Errorf("no agent configuration file found in %s", dir)
}
