package toolinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
)

// githubToken returns a GitHub personal access token from the environment,
// checking GITHUB_TOKEN first, then GH_TOKEN (used by the gh CLI).
// Returns an empty string if neither is set.
func githubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

// setGitHubAuth adds a Bearer authorization header to the request
// if a GitHub token is available in the environment. This raises the
// GitHub API rate limit from 60 to 5,000 requests per hour.
func setGitHubAuth(req *http.Request) {
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

const (
	registryBaseURL  = "https://raw.githubusercontent.com/aquaproj/aqua-registry/main"
	registryCacheTTL = 24 * time.Hour
)

// Package represents a parsed aqua registry package definition.
type Package struct {
	Type          string            `yaml:"type"`
	RepoOwner     string            `yaml:"repo_owner"`
	RepoName      string            `yaml:"repo_name"`
	Description   string            `yaml:"description"`
	Asset         string            `yaml:"asset"`
	Format        string            `yaml:"format"`
	Files         []PackageFile     `yaml:"files"`
	Overrides     []Override        `yaml:"overrides"`
	Replacements  map[string]string `yaml:"replacements"`
	SupportedEnvs []string          `yaml:"supported_envs"`
	VersionFilter string            `yaml:"version_filter"`
	VersionPrefix string            `yaml:"version_prefix"`
	Name          string            `yaml:"name"`

	// GoInstallPath is the Go module path for go_install/go_build packages.
	// Example: "golang.org/x/tools/gopls"
	GoInstallPath string `yaml:"path"`
}

// IsGoPackage returns true if this package is installed via "go install".
func (p *Package) IsGoPackage() bool {
	switch p.Type {
	case "go_install", "go", "go_build":
		return true
	default:
		return false
	}
}

// BinaryName returns the primary binary name this package provides.
func (p *Package) BinaryName() string {
	if len(p.Files) > 0 {
		return p.Files[0].Name
	}
	return p.RepoName
}

// PackageFile describes a file within a downloaded archive.
type PackageFile struct {
	Name string `yaml:"name"`
	Src  string `yaml:"src"`
}

// Override represents a platform-specific override for a package.
type Override struct {
	GOOS         string            `yaml:"goos"`
	GOArch       string            `yaml:"goarch"`
	Asset        string            `yaml:"asset"`
	Format       string            `yaml:"format"`
	Files        []PackageFile     `yaml:"files"`
	Replacements map[string]string `yaml:"replacements"`
}

// registryIndex represents the top-level registry.yaml containing full package definitions.
type registryIndex struct {
	Packages []Package `yaml:"packages"`
}

// Registry provides lookup of aqua packages.
type Registry struct {
	httpClient *http.Client
	baseURL    string
	cacheDir   string

	// In-memory cache for the parsed registry index, populated once via sync.Once.
	indexOnce   sync.Once
	cachedIndex *registryIndex
	indexErr    error
}

var (
	sharedRegistry     *Registry
	sharedRegistryOnce sync.Once
)

// NewRegistry creates a new Registry with default settings.
func NewRegistry() *Registry {
	return &Registry{
		httpClient: http.DefaultClient,
		baseURL:    registryBaseURL,
		cacheDir:   RegistryDir(),
	}
}

// SharedRegistry returns a package-level Registry instance that is reused
// across all tool resolutions within a docker agent session, avoiding repeated
// YAML parsing and HTTP fetches.
func SharedRegistry() *Registry {
	sharedRegistryOnce.Do(func() {
		sharedRegistry = NewRegistry()
	})
	return sharedRegistry
}

// LookupByName searches the registry for a package by "owner/repo" identifier.
// Searches the full registry index first, then falls back to fetching the
// per-package YAML from pkgs/<owner>/<repo>/registry.yaml.
func (r *Registry) LookupByName(ctx context.Context, name string) (*Package, error) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid aqua package name %q: expected owner/repo format", name)
	}

	owner, repo := parts[0], parts[1]

	// Try the full registry index first.
	index, err := r.fetchIndex(ctx)
	if err == nil {
		for i := range index.Packages {
			p := &index.Packages[i]
			if strings.EqualFold(p.RepoOwner, owner) && strings.EqualFold(p.RepoName, repo) {
				return p, nil
			}
		}
	}

	// Fallback: fetch the per-package YAML file.
	data, err := r.fetchCached(ctx, fmt.Sprintf("pkgs/%s/%s/registry.yaml", owner, repo), 0)
	if err != nil {
		return nil, fmt.Errorf("fetching package %s: %w", name, err)
	}

	var wrapper registryIndex
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("parsing package YAML for %s: %w", name, err)
	}

	if len(wrapper.Packages) == 0 {
		return nil, fmt.Errorf("no packages found in registry for %s", name)
	}

	return &wrapper.Packages[0], nil
}

// LookupByCommand searches for a package providing a binary matching the command name.
// First checks repo names, then files[].name across all packages.
func (r *Registry) LookupByCommand(ctx context.Context, command string) (*Package, error) {
	index, err := r.fetchIndex(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching registry index: %w", err)
	}

	// Single pass: prefer repo name match, but track the first file-name match.
	var fileMatch *Package
	for i := range index.Packages {
		p := &index.Packages[i]
		if p.RepoName == command {
			return p, nil
		}
		if fileMatch == nil && providesCommand(p, command) {
			fileMatch = p
		}
	}

	if fileMatch != nil {
		return fileMatch, nil
	}

	return nil, fmt.Errorf("no aqua package found providing command %q", command)
}

// providesCommand returns true if any of the package's files entries has
// a binary matching the given command name.
func providesCommand(pkg *Package, command string) bool {
	for _, f := range pkg.Files {
		if f.Name == command {
			return true
		}
	}
	return false
}

// fetchIndex fetches and parses the full registry index, with caching.
// The parsed result is cached in memory so that repeated calls within the
// same Registry instance skip both the HTTP fetch and YAML deserialization.
func (r *Registry) fetchIndex(ctx context.Context) (*registryIndex, error) {
	r.indexOnce.Do(func() {
		var data []byte
		data, r.indexErr = r.fetchCached(ctx, "registry.yaml", registryCacheTTL)
		if r.indexErr != nil {
			return
		}

		var index registryIndex
		if err := yaml.Unmarshal(data, &index); err != nil {
			r.indexErr = fmt.Errorf("parsing registry index: %w", err)
			return
		}
		r.cachedIndex = &index
	})

	return r.cachedIndex, r.indexErr
}

// fetchCached fetches a file from the registry, using a local file cache.
// A ttl of 0 means the cache never expires.
func (r *Registry) fetchCached(ctx context.Context, path string, ttl time.Duration) ([]byte, error) {
	cachePath := filepath.Join(r.cacheDir, path)

	// Return cached data if still fresh.
	if info, err := os.Stat(cachePath); err == nil {
		if ttl == 0 || time.Since(info.ModTime()) < ttl {
			return os.ReadFile(cachePath)
		}
	}

	// Fetch from remote.
	url := r.baseURL + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			return data, nil
		}
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}
	setGitHubAuth(req)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			return data, nil // stale cache beats no data
		}
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if data, readErr := os.ReadFile(cachePath); readErr == nil {
			return data, nil
		}
		return nil, fmt.Errorf("fetching %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", url, err)
	}

	// Write to cache atomically (best-effort): write to a temp file in the
	// same directory, then rename. This avoids races when multiple goroutines
	// fetch the same path concurrently.
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
		if tmpFile, tmpErr := os.CreateTemp(filepath.Dir(cachePath), ".cache-*.tmp"); tmpErr == nil {
			if _, writeErr := tmpFile.Write(data); writeErr == nil {
				tmpFile.Close()
				_ = os.Rename(tmpFile.Name(), cachePath)
			} else {
				tmpFile.Close()
				_ = os.Remove(tmpFile.Name())
			}
		}
	}

	return data, nil
}

// githubRelease represents the relevant fields from the GitHub releases API.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// latestVersion fetches the latest release version for a GitHub repo.
func (r *Registry) latestVersion(ctx context.Context, owner, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	var release githubRelease
	if err := r.fetchGitHubJSON(ctx, url, &release); err != nil {
		return "", fmt.Errorf("fetching latest release for %s/%s: %w", owner, repo, err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no tag_name found in latest release for %s/%s", owner, repo)
	}

	return release.TagName, nil
}

// latestVersionFiltered fetches the latest release version matching a tag prefix.
// Needed for multi-module repos like golang/tools where tags look like "gopls/v0.21.1".
func (r *Registry) latestVersionFiltered(ctx context.Context, owner, repo, tagPrefix string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases?per_page=50", owner, repo)

	var releases []githubRelease
	if err := r.fetchGitHubJSON(ctx, url, &releases); err != nil {
		return "", fmt.Errorf("fetching releases for %s/%s: %w", owner, repo, err)
	}

	for _, rel := range releases {
		if strings.HasPrefix(rel.TagName, tagPrefix) {
			return rel.TagName, nil
		}
	}

	return "", fmt.Errorf("no release found for %s/%s with tag prefix %q", owner, repo, tagPrefix)
}

// fetchGitHubJSON fetches a GitHub API endpoint and decodes the JSON response.
func (r *Registry) fetchGitHubJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	setGitHubAuth(req)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// download opens an HTTP connection to the given URL and returns the
// response body as an io.ReadCloser. The caller is responsible for closing it.
func (r *Registry) download(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	setGitHubAuth(req)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return resp.Body, nil
}
