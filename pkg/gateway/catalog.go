package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	DockerCatalogURL     = "https://desktop.docker.com/mcp/catalog/v3/catalog.yaml"
	catalogCacheFileName = "mcp_catalog.json"
	catalogCacheDuration = 24 * time.Hour
)

func RequiredEnvVars(ctx context.Context, serverName string) ([]Secret, error) {
	server, err := ServerSpec(ctx, serverName)
	if err != nil {
		return nil, err
	}

	// TODO(dga): until the MCP Gateway supports oauth with docker agent,
	// we ignore every secret listed on `remote` servers and assume
	// we can use oauth by connecting directly to the server's url.
	if server.Type == "remote" {
		return nil, nil
	}

	return server.Secrets, nil
}

func ServerSpec(_ context.Context, serverName string) (Server, error) {
	server, ok := getCatalogServer(serverName)
	if !ok {
		return Server{}, fmt.Errorf("MCP server %q not found in MCP catalog", serverName)
	}
	return server, nil
}

type cachedCatalog struct {
	Catalog  Catalog   `json:"catalog"`
	CachedAt time.Time `json:"cached_at"`
}

var (
	catalogMu     sync.RWMutex
	catalogData   Catalog
	catalogLoaded bool
	catalogStale  bool
	refreshOnce   sync.Once
)

// getCatalogServer returns a server from the catalog, refreshing if needed.
// If server is not found in cache, it will try to fetch fresh data from network
// in case it's a newly added server.
func getCatalogServer(serverName string) (Server, bool) {
	// First, ensure catalog is loaded
	ensureCatalogLoaded()

	catalogMu.RLock()
	server, ok := catalogData[serverName]
	stale := catalogStale
	catalogMu.RUnlock()

	if ok {
		// Found in cache. If stale, trigger background refresh for next time.
		if stale {
			triggerBackgroundRefresh()
		}
		return server, true
	}

	// Server not found in cache. Try fetching fresh data in case it's a new server.
	if refreshCatalogFromNetwork() {
		catalogMu.RLock()
		server, ok = catalogData[serverName]
		catalogMu.RUnlock()
		return server, ok
	}

	return Server{}, false
}

// ensureCatalogLoaded loads the catalog from cache or network on first access.
func ensureCatalogLoaded() {
	catalogMu.RLock()
	loaded := catalogLoaded
	catalogMu.RUnlock()

	if loaded {
		return
	}

	catalogMu.Lock()
	defer catalogMu.Unlock()

	// Double-check after acquiring write lock
	if catalogLoaded {
		return
	}

	cacheFile := getCacheFilePath()

	// Try loading from local cache first
	if cached, cacheAge, err := loadCatalogFromCache(cacheFile); err == nil {
		slog.Debug("Loaded MCP catalog from cache", "file", cacheFile, "age", cacheAge.Round(time.Second))
		catalogData = cached
		catalogLoaded = true
		catalogStale = cacheAge > catalogCacheDuration
		return
	}

	// Cache miss or invalid, fetch from network
	catalog, err := fetchCatalogFromNetwork()
	if err != nil {
		slog.Error("Failed to fetch MCP catalog", "error", err)
		return
	}

	catalogData = catalog
	catalogLoaded = true
	catalogStale = false

	// Save to cache (best effort)
	if err := saveCatalogToCache(cacheFile, catalog); err != nil {
		slog.Warn("Failed to save MCP catalog to cache", "error", err)
	}
}

// triggerBackgroundRefresh starts a background goroutine to refresh the catalog.
// Only one background refresh will run at a time.
func triggerBackgroundRefresh() {
	refreshOnce.Do(func() {
		go func() {
			refreshCatalogFromNetwork()
			// Reset refreshOnce so future stale reads can trigger another refresh
			refreshOnce = sync.Once{}
		}()
	})
}

// refreshCatalogFromNetwork fetches fresh catalog data and updates the cache.
// Returns true if refresh was successful.
func refreshCatalogFromNetwork() bool {
	catalog, err := fetchCatalogFromNetwork()
	if err != nil {
		slog.Debug("Background catalog refresh failed", "error", err)
		return false
	}

	catalogMu.Lock()
	catalogData = catalog
	catalogStale = false
	catalogMu.Unlock()

	// Save to cache (best effort)
	if err := saveCatalogToCache(getCacheFilePath(), catalog); err != nil {
		slog.Warn("Failed to save refreshed MCP catalog to cache", "error", err)
	}

	slog.Debug("MCP catalog refreshed from network")
	return true
}

func getCacheFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, ".cagent", catalogCacheFileName)
}

func loadCatalogFromCache(cacheFile string) (Catalog, time.Duration, error) {
	if cacheFile == "" {
		return nil, 0, fmt.Errorf("no cache file path")
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cached cachedCatalog
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, 0, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	cacheAge := time.Since(cached.CachedAt)
	return cached.Catalog, cacheAge, nil
}

func saveCatalogToCache(cacheFile string, catalog Catalog) error {
	if cacheFile == "" {
		return nil
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0o755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	cached := cachedCatalog{
		Catalog:  catalog,
		CachedAt: time.Now(),
	}

	data, err := json.Marshal(cached)
	if err != nil {
		return fmt.Errorf("failed to marshal cached data: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

func fetchCatalogFromNetwork() (Catalog, error) {
	// Use the JSON version because it's 3x time faster to parse than YAML.
	url := strings.Replace(DockerCatalogURL, ".yaml", ".json", 1)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL: %s, status: %s", url, resp.Status)
	}

	var topLevel topLevel
	if err := json.NewDecoder(resp.Body).Decode(&topLevel); err != nil {
		return nil, err
	}

	return topLevel.Catalog, nil
}
