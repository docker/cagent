package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ModelsDevAPIURL = "https://models.dev/api.json"
	CacheFileName   = "models_dev.json"
)

// ModelAliases maps alias model IDs to their actual model IDs
var ModelAliases = map[string]string{
	"anthropic/claude-sonnet-4-0": "anthropic/claude-sonnet-4-20250514",
	"anthropic/claude-sonnet-4-5": "anthropic/claude-sonnet-4-5-20250929",
	"anthropic/claude-haiku-4-5":  "nthropic/claude-haiku-4-5-20251001",
}

// Store manages the models.dev data with local caching
type Store struct {
	cacheDir        string
	client          *http.Client
	refreshInterval time.Duration
}

type Opt func(*Store)

func WithRefreshInterval(refreshInterval time.Duration) Opt {
	return func(s *Store) {
		s.refreshInterval = refreshInterval
	}
}

func WithCacheDir(cacheDir string) Opt {
	return func(s *Store) {
		s.cacheDir = cacheDir
	}
}

// NewStore creates a new models.dev store instance
func NewStore(opts ...Opt) (*Store, error) {
	s := &Store{
		refreshInterval: 24 * time.Hour,
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cagent")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}
	s.cacheDir = cacheDir
	for _, opt := range opts {
		opt(s)
	}

	s.client = &http.Client{
		Timeout: 30 * time.Second,
	}

	return s, nil
}

// GetDatabase returns the models.dev database, fetching from cache or API as needed
func (s *Store) GetDatabase(ctx context.Context) (*Database, error) {
	cacheFile := filepath.Join(s.cacheDir, CacheFileName)

	// Try to load from cache first
	cached, err := s.loadFromCache(cacheFile)
	if err == nil && s.isCacheValid(cached) {
		return &cached.Database, nil
	}

	// Cache is invalid or doesn't exist, fetch from API
	database, err := s.fetchFromAPI(ctx)
	if err != nil {
		// If API fetch fails, but we have cached data, use it
		if cached != nil {
			return &cached.Database, nil
		}
		return nil, fmt.Errorf("failed to fetch from API and no cached data available: %w", err)
	}

	// Save to cache
	if err := s.saveToCache(cacheFile, database); err != nil {
		// Log the error but don't fail the request
		fmt.Printf("Warning: failed to save to cache: %v\n", err)
	}

	return database, nil
}

// GetProvider returns a specific provider by ID
func (s *Store) GetProvider(ctx context.Context, providerID string) (*Provider, error) {
	db, err := s.GetDatabase(ctx)
	if err != nil {
		return nil, err
	}

	provider, exists := db.Providers[providerID]
	if !exists {
		return nil, fmt.Errorf("provider %q not found", providerID)
	}

	return &provider, nil
}

// GetModel returns a specific model by provider ID and model ID
func (s *Store) GetModel(ctx context.Context, id string) (*Model, error) {
	// Check if the ID is an alias and resolve it
	if actualID, isAlias := ModelAliases[id]; isAlias {
		id = actualID
	}

	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid model ID: %q", id)
	}
	providerID := parts[0]
	modelID := parts[1]

	provider, err := s.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}

	model, exists := provider.Models[modelID]
	if !exists {
		return nil, fmt.Errorf("model %q not found in provider %q", modelID, providerID)
	}

	return &model, nil
}

func (s *Store) fetchFromAPI(ctx context.Context) (*Database, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelsDevAPIURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var providers map[string]Provider
	if err := json.Unmarshal(body, &providers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	database := &Database{
		Providers: providers,
		UpdatedAt: time.Now(),
	}

	return database, nil
}

func (s *Store) loadFromCache(cacheFile string) (*CachedData, error) {
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cached CachedData
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cached data: %w", err)
	}

	return &cached, nil
}

func (s *Store) saveToCache(cacheFile string, database *Database) error {
	now := time.Now()
	cached := CachedData{
		Database:    *database,
		CachedAt:    now,
		LastRefresh: now,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cached data: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0o644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

func (s *Store) isCacheValid(cached *CachedData) bool {
	return time.Since(cached.LastRefresh) < s.refreshInterval
}
