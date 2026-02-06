package config

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestURLSource_Read(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("test content"))
	}))
	t.Cleanup(server.Close)

	source := NewURLSource(server.URL, nil)

	assert.Equal(t, server.URL, source.Name())
	assert.Empty(t, source.ParentDir())

	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "test content", string(data))
}

func TestURLSource_Read_HTTPError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
	}{
		{"not found", http.StatusNotFound},
		{"server error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			t.Cleanup(server.Close)

			// Clean up any cached data for this URL to ensure we test the error path
			urlCacheDir := getURLCacheDir()
			urlHash := hashURL(server.URL)
			cachePath := filepath.Join(urlCacheDir, urlHash)
			etagPath := cachePath + ".etag"
			_ = os.Remove(cachePath)
			_ = os.Remove(etagPath)

			_, err := NewURLSource(server.URL, nil).Read(t.Context())
			require.Error(t, err)
		})
	}
}

func TestURLSource_Read_ConnectionError(t *testing.T) {
	t.Parallel()

	_, err := NewURLSource("http://invalid.invalid/config.yaml", nil).Read(t.Context())
	require.Error(t, err)
}

func TestURLSource_Read_CachesContent(t *testing.T) {
	// Not parallel - uses shared cache directory

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"test-etag-caches-content"`)
		_, _ = w.Write([]byte("test content for caching"))
	}))
	t.Cleanup(server.Close)

	source := NewURLSource(server.URL, nil)

	// First read should fetch and cache
	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "test content for caching", string(data))

	// Verify cache files were created
	urlCacheDir := getURLCacheDir()
	urlHash := hashURL(server.URL)
	cachePath := filepath.Join(urlCacheDir, urlHash)
	etagPath := cachePath + ".etag"

	// Cleanup at end of test
	t.Cleanup(func() {
		_ = os.Remove(cachePath)
		_ = os.Remove(etagPath)
	})

	cachedData, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Equal(t, "test content for caching", string(cachedData))

	cachedETag, err := os.ReadFile(etagPath)
	require.NoError(t, err)
	assert.Equal(t, `"test-etag-caches-content"`, string(cachedETag))
}

func TestURLSource_Read_UsesETagForConditionalRequest(t *testing.T) {
	// Not parallel - uses shared cache directory

	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.Header.Get("If-None-Match") == `"test-etag-conditional"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"test-etag-conditional"`)
		_, _ = w.Write([]byte("test content conditional"))
	}))
	t.Cleanup(server.Close)

	// Pre-populate cache
	urlCacheDir := getURLCacheDir()
	require.NoError(t, os.MkdirAll(urlCacheDir, 0o755))
	urlHash := hashURL(server.URL)
	cachePath := filepath.Join(urlCacheDir, urlHash)
	etagPath := cachePath + ".etag"
	require.NoError(t, os.WriteFile(cachePath, []byte("cached content conditional"), 0o644))
	require.NoError(t, os.WriteFile(etagPath, []byte(`"test-etag-conditional"`), 0o644))

	// Cleanup at end of test
	t.Cleanup(func() {
		_ = os.Remove(cachePath)
		_ = os.Remove(etagPath)
	})

	source := NewURLSource(server.URL, nil)

	// Read should use cached content via 304 response
	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "cached content conditional", string(data))
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestURLSource_Read_FallsBackToCacheOnNetworkError(t *testing.T) {
	// Not parallel - uses shared cache directory

	// Pre-populate cache for a non-existent server
	url := "http://invalid.invalid:12345/config-network-error.yaml"
	urlCacheDir := getURLCacheDir()
	require.NoError(t, os.MkdirAll(urlCacheDir, 0o755))
	urlHash := hashURL(url)
	cachePath := filepath.Join(urlCacheDir, urlHash)
	require.NoError(t, os.WriteFile(cachePath, []byte("cached content network error"), 0o644))

	// Cleanup at end of test
	t.Cleanup(func() {
		_ = os.Remove(cachePath)
	})

	source := NewURLSource(url, nil)

	// Read should fall back to cached content
	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "cached content network error", string(data))
}

func TestURLSource_Read_FallsBackToCacheOnHTTPError(t *testing.T) {
	// Not parallel - uses shared cache directory

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	// Pre-populate cache
	urlCacheDir := getURLCacheDir()
	require.NoError(t, os.MkdirAll(urlCacheDir, 0o755))
	urlHash := hashURL(server.URL)
	cachePath := filepath.Join(urlCacheDir, urlHash)
	require.NoError(t, os.WriteFile(cachePath, []byte("cached content http error"), 0o644))

	// Cleanup at end of test
	t.Cleanup(func() {
		_ = os.Remove(cachePath)
	})

	source := NewURLSource(server.URL, nil)

	// Read should fall back to cached content
	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "cached content http error", string(data))
}

func TestURLSource_Read_UpdatesCacheWhenContentChanges(t *testing.T) {
	// Not parallel - uses shared cache directory

	var content atomic.Value
	content.Store("initial content update")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentContent := content.Load().(string)
		etag := `"etag-` + currentContent + `"`

		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		_, _ = w.Write([]byte(currentContent))
	}))
	t.Cleanup(server.Close)

	urlCacheDir := getURLCacheDir()
	urlHash := hashURL(server.URL)
	cachePath := filepath.Join(urlCacheDir, urlHash)
	etagPath := cachePath + ".etag"

	// Cleanup at end of test
	t.Cleanup(func() {
		_ = os.Remove(cachePath)
		_ = os.Remove(etagPath)
	})

	source := NewURLSource(server.URL, nil)

	// First read
	data, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "initial content update", string(data))

	// Change content
	content.Store("updated content update")

	// Second read should get new content
	data, err = source.Read(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "updated content update", string(data))

	// Verify cache was updated
	cachedData, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	assert.Equal(t, "updated content update", string(cachedData))
}

func TestIsURLReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		{"http://example.com/agent.yaml", true},
		{"https://example.com/agent.yaml", true},
		{"https://example.com:8080/path", true},
		{"/path/to/agent.yaml", false},
		{"./agent.yaml", false},
		{"docker.io/myorg/agent:v1", false},
		{"ftp://example.com/agent.yaml", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, IsURLReference(tt.input))
		})
	}
}

func TestResolve_URLReference(t *testing.T) {
	t.Parallel()

	source, err := Resolve("https://example.com/agent.yaml", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/agent.yaml", source.Name())
	assert.Empty(t, source.ParentDir())
}

func TestResolveSources_URLReference(t *testing.T) {
	t.Parallel()

	url := "https://example.com/agent.yaml"
	sources, err := ResolveSources(url, nil)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	source, ok := sources[url]
	require.True(t, ok)
	assert.Equal(t, url, source.Name())
}

func TestURLSource_Read_WithGitHubAuth(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("test content"))
	}))
	t.Cleanup(server.Close)

	// Create a mock env provider that returns a GitHub token
	envProvider := &mockEnvProvider{
		envVars: map[string]string{
			"GITHUB_TOKEN": "test-token-123",
		},
	}

	// For non-GitHub URLs, auth should not be added even with token available
	source := NewURLSource(server.URL, envProvider)
	_, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "non-GitHub URLs should not receive auth header")
}

func TestURLSource_Read_WithGitHubAuth_GitHubURL(t *testing.T) {
	t.Parallel()

	// Note: We cannot directly test with real GitHub URLs in unit tests.
	// This test verifies that URLs with GitHub hosts in the path (not hostname)
	// are correctly identified as non-GitHub URLs and don't receive auth.
	// This is a security-critical behavior to prevent token leakage.

	for _, host := range githubHosts {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			var receivedAuth string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				receivedAuth = r.Header.Get("Authorization")
				_, _ = w.Write([]byte("test content"))
			}))
			t.Cleanup(server.Close)

			envProvider := &mockEnvProvider{
				envVars: map[string]string{
					"GITHUB_TOKEN": "test-token-456",
				},
			}

			// URL with GitHub host in path (not hostname) should NOT receive auth
			// This prevents token leakage to attacker-controlled domains
			maliciousURL := server.URL + "/" + host + "/path/to/file"
			source := NewURLSource(maliciousURL, envProvider)

			_, err := source.Read(t.Context())
			require.NoError(t, err)
			assert.Empty(t, receivedAuth, "should not add auth header when GitHub host is only in path")
		})
	}
}

func TestURLSource_Read_WithGitHubAuth_NoToken(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("test content"))
	}))
	t.Cleanup(server.Close)

	// Create a mock env provider without a GitHub token
	envProvider := &mockEnvProvider{
		envVars: map[string]string{},
	}

	source := NewURLSource(server.URL, envProvider)
	_, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "should not add auth header when token is missing")
}

func TestURLSource_Read_WithGitHubAuth_NoEnvProvider(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("test content"))
	}))
	t.Cleanup(server.Close)

	// No env provider
	source := NewURLSource(server.URL, nil)
	_, err := source.Read(t.Context())
	require.NoError(t, err)
	assert.Empty(t, receivedAuth, "should not add auth header without env provider")
}

func TestIsGitHubURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		url      string
		expected bool
	}{
		// Valid GitHub URLs
		{"https://github.com/owner/repo/blob/main/agent.yaml", true},
		{"https://raw.githubusercontent.com/owner/repo/main/agent.yaml", true},
		{"https://gist.githubusercontent.com/owner/gist-id/raw/file.yaml", true},
		{"http://github.com/owner/repo", true},

		// Non-GitHub URLs
		{"https://example.com/agent.yaml", false},
		{"https://gitlab.com/owner/repo/agent.yaml", false},
		{"http://localhost:8080/agent.yaml", false},
		{"", false},

		// Security: malicious URLs that should NOT be treated as GitHub URLs
		// These test cases prevent token leakage to attacker-controlled domains
		{"https://evil.com/github.com/file.yaml", false},           // github.com in path
		{"https://notgithub.com/file.yaml", false},                 // similar domain name
		{"https://github.com.attacker.com/file.yaml", false},       // github.com as subdomain
		{"https://fakegithub.com/owner/repo/agent.yaml", false},    // contains "github.com" substring
		{"https://raw.githubusercontent.com.evil.com/file", false}, // githubusercontent as subdomain
		{"https://attacker.com?redirect=github.com", false},        // github.com in query string
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, isGitHubURL(tt.url))
		})
	}
}

func TestResolve_URLReference_WithEnvProvider(t *testing.T) {
	t.Parallel()

	envProvider := &mockEnvProvider{
		envVars: map[string]string{
			"GITHUB_TOKEN": "test-token",
		},
	}

	source, err := Resolve("https://github.com/owner/repo/raw/main/agent.yaml", envProvider)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/owner/repo/raw/main/agent.yaml", source.Name())

	// Verify the source has the env provider set
	urlSrc, ok := source.(*urlSource)
	require.True(t, ok)
	assert.NotNil(t, urlSrc.envProvider)
}

func TestResolveSources_URLReference_WithEnvProvider(t *testing.T) {
	t.Parallel()

	envProvider := &mockEnvProvider{
		envVars: map[string]string{
			"GITHUB_TOKEN": "test-token",
		},
	}

	url := "https://github.com/owner/repo/raw/main/agent.yaml"
	sources, err := ResolveSources(url, envProvider)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	source, ok := sources[url]
	require.True(t, ok)

	// Verify the source has the env provider set
	urlSrc, ok := source.(*urlSource)
	require.True(t, ok)
	assert.NotNil(t, urlSrc.envProvider)
}
