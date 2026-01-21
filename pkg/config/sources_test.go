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

	source := NewURLSource(server.URL)

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

			_, err := NewURLSource(server.URL).Read(t.Context())
			require.Error(t, err)
		})
	}
}

func TestURLSource_Read_ConnectionError(t *testing.T) {
	t.Parallel()

	_, err := NewURLSource("http://invalid.invalid/config.yaml").Read(t.Context())
	require.Error(t, err)
}

func TestURLSource_Read_CachesContent(t *testing.T) {
	// Not parallel - uses shared cache directory

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"test-etag-caches-content"`)
		_, _ = w.Write([]byte("test content for caching"))
	}))
	t.Cleanup(server.Close)

	source := NewURLSource(server.URL)

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

	source := NewURLSource(server.URL)

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

	source := NewURLSource(url)

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

	source := NewURLSource(server.URL)

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

	source := NewURLSource(server.URL)

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

	source, err := Resolve("https://example.com/agent.yaml")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/agent.yaml", source.Name())
	assert.Empty(t, source.ParentDir())
}

func TestResolveSources_URLReference(t *testing.T) {
	t.Parallel()

	url := "https://example.com/agent.yaml"
	sources, err := ResolveSources(url)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	source, ok := sources[url]
	require.True(t, ok)
	assert.Equal(t, url, source.Name())
}
