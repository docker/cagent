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
	"regexp"
	"strings"

	"github.com/docker/cagent/pkg/content"
	"github.com/docker/cagent/pkg/environment"
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
	for _, host := range githubHosts {
		if u.Host == host {
			return true
		}
	}
	return false
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

// gitSource is used to load an agent configuration from a Git repository.
type gitSource struct {
	repoURL string
	ref     string // branch, tag, or commit (empty means default branch)
}

// sshGitPattern matches SSH git URLs like git@github.com:user/repo.git
var sshGitPattern = regexp.MustCompile(`^git@([^:]+):(.+?)(\.git)?$`)

// httpsGitPattern matches HTTPS git URLs like https://github.com/user/repo.git
var httpsGitPattern = regexp.MustCompile(`^https://([^/]+)/(.+?)(\.git)?$`)

// ParseGitReference parses a git reference string and returns the repo URL and ref.
// Supported formats:
// - git@github.com:user/repo.git (SSH)
// - git@github.com:user/repo.git#branch (SSH with ref)
// - https://github.com/user/repo.git (HTTPS)
// - https://github.com/user/repo.git#tag (HTTPS with ref)
func ParseGitReference(input string) (repoURL, ref string, ok bool) {
	// Split off the ref if present
	parts := strings.SplitN(input, "#", 2)
	mainPart := parts[0]
	if len(parts) == 2 {
		ref = parts[1]
	}
	// Check SSH format
	if sshGitPattern.MatchString(mainPart) {
		return mainPart, ref, true
	}
	// Check HTTPS format with .git suffix
	if httpsGitPattern.MatchString(mainPart) && strings.HasSuffix(mainPart, ".git") {
		return mainPart, ref, true
	}
	return "", "", false
}

// IsGitReference checks if the input is a valid Git repository reference.
func IsGitReference(input string) bool {
	_, _, ok := ParseGitReference(input)
	return ok
}

// NewGitSource creates a new Git source from a git reference string.
func NewGitSource(input string) Source {
	repoURL, ref, _ := ParseGitReference(input)
	return &gitSource{
		repoURL: repoURL,
		ref:     ref,
	}
}

func (g *gitSource) Name() string {
	if g.ref != "" {
		return g.repoURL + "#" + g.ref
	}
	return g.repoURL
}

func (g *gitSource) ParentDir() string {
	return g.getCacheDir()
}

// getGitCacheDir returns the base directory used to cache Git repositories.
func getGitCacheDir() string {
	return filepath.Join(paths.GetDataDir(), "git_cache")
}

func (g *gitSource) getCacheDir() string {
	// Create a unique directory name based on repo URL and ref
	// Include ref in the cache key to prevent race conditions when
	// multiple refs of the same repo are accessed concurrently
	cacheKey := g.repoURL
	if g.ref != "" {
		cacheKey = g.repoURL + "#" + g.ref
	}
	h := sha256.Sum256([]byte(cacheKey))
	return filepath.Join(getGitCacheDir(), hex.EncodeToString(h[:16]))
}

func (g *gitSource) Read(ctx context.Context) ([]byte, error) {
	cacheDir := g.getCacheDir()
	// Check if repo is already cloned
	gitPath := filepath.Join(cacheDir, ".git")
	info, err := os.Stat(gitPath)
	if err == nil && info.IsDir() {
		if err := g.fetchAndCheckout(ctx, cacheDir); err != nil {
			slog.Debug("Failed to fetch git repo, using cached version", "repo", g.repoURL, "error", err)
		}
	} else {
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("checking git repository cache %s: %w", gitPath, err)
		}
		if err := g.clone(ctx, cacheDir); err != nil {
			return nil, fmt.Errorf("cloning git repository %s: %w", g.repoURL, err)
		}
	}
	// Read agent.yaml from the repo root
	configPath := filepath.Join(cacheDir, "agent.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading agent.yaml from %s: %w", g.repoURL, err)
	}
	return data, nil
}

func (g *gitSource) clone(ctx context.Context, targetDir string) error {
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	args := []string{"clone", "--depth", "1"}
	if g.ref != "" {
		args = append(args, "--branch", g.ref)
	}
	args = append(args, g.repoURL, targetDir)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\noutput: %s", err, string(output))
	}
	slog.Debug("Cloned git repository", "repo", g.repoURL, "ref", g.ref, "dir", targetDir)
	return nil
}

func (g *gitSource) fetchAndCheckout(ctx context.Context, repoDir string) error {
	// Fetch latest changes - specify the ref for shallow clones
	fetchArgs := []string{"fetch", "--depth", "1", "origin"}
	if g.ref != "" {
		fetchArgs = append(fetchArgs, g.ref)
	}
	fetchCmd := exec.CommandContext(ctx, "git", fetchArgs...)
	fetchCmd.Dir = repoDir
	fetchCmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w\noutput: %s", err, string(output))
	}
	// Determine target ref - use FETCH_HEAD for explicit refs since we fetched it directly
	var targetRef string
	if g.ref == "" {
		targetRef = "origin/HEAD"
	} else {
		targetRef = "FETCH_HEAD"
	}
	// Reset to the target ref
	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", targetRef)
	resetCmd.Dir = repoDir
	if output, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git reset failed: %w\noutput: %s", err, string(output))
	}
	slog.Debug("Updated git repository", "repo", g.repoURL, "ref", g.ref, "dir", repoDir)
	return nil
}
