package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	ModelsDevAPIURL = "https://models.dev/api.json"
	CacheFileName   = "models_dev.json"
)

// ModelAliases maps alias model IDs to their actual model IDs
// TODO(krissetto): Add aliases here if needed, removed if unused
var ModelAliases = map[string]string{}

// Store manages the models.dev data with local caching
type Store struct {
	cacheDir        string
	client          *http.Client
	refreshInterval time.Duration

	// In-memory cache for database to avoid repeated disk reads
	dbCache   *Database
	dbCacheMu sync.RWMutex
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

// defaultStore is a cached singleton store instance for repeated access
var defaultStore = sync.OnceValues(func() (*Store, error) {
	return newStoreInternal()
})

// NewStore returns the cached default store instance.
// This is efficient for repeated calls as it reuses the same store.
// For custom configuration, use NewStoreWithOptions.
func NewStore(opts ...Opt) (*Store, error) {
	if len(opts) > 0 {
		return newStoreInternal(opts...)
	}
	return defaultStore()
}

// newStoreInternal creates a new models.dev store instance
func newStoreInternal(opts ...Opt) (*Store, error) {
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

// GetDatabase returns the models.dev database, fetching from cache or API as needed.
// Results are cached in memory to avoid repeated disk reads within the same process.
func (s *Store) GetDatabase(ctx context.Context) (*Database, error) {
	// Check in-memory cache first
	s.dbCacheMu.RLock()
	if s.dbCache != nil {
		db := s.dbCache
		s.dbCacheMu.RUnlock()
		return db, nil
	}
	s.dbCacheMu.RUnlock()

	// Need to load from disk or network
	s.dbCacheMu.Lock()
	defer s.dbCacheMu.Unlock()

	// Double-check after acquiring write lock
	if s.dbCache != nil {
		return s.dbCache, nil
	}

	cacheFile := filepath.Join(s.cacheDir, CacheFileName)

	// Try to load from cache first
	cached, err := s.loadFromCache(cacheFile)
	if err == nil && s.isCacheValid(cached) {
		s.dbCache = &cached.Database
		return s.dbCache, nil
	}

	// Cache is invalid or doesn't exist, fetch from API
	database, err := s.fetchFromAPI(ctx)
	if err != nil {
		// If API fetch fails, but we have cached data, use it
		if cached != nil {
			s.dbCache = &cached.Database
			return s.dbCache, nil
		}
		return nil, fmt.Errorf("failed to fetch from API and no cached data available: %w", err)
	}

	// Save to cache
	if err := s.saveToCache(cacheFile, database); err != nil {
		// Log the error but don't fail the request
		slog.Warn("Warning: failed to save to cache", "error", err)
	}

	s.dbCache = database
	return s.dbCache, nil
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
		// For amazon-bedrock, try stripping region/inference profile prefixes
		// Bedrock uses prefixes for cross-region inference profiles,
		// but models.dev stores models without these prefixes.
		//
		// Strip known region prefixes and retry lookup.
		if providerID == "amazon-bedrock" {
			if idx := strings.Index(modelID, "."); idx != -1 {
				possibleRegionPrefix := modelID[:idx]
				if isBedrockRegionPrefix(possibleRegionPrefix) {
					normalizedModelID := modelID[idx+1:]
					model, exists = provider.Models[normalizedModelID]
					if exists {
						return &model, nil
					}
				}
			}
		}
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

// SetDatabaseForTesting sets the in-memory database cache for testing purposes.
// This method should only be used in tests.
func (s *Store) SetDatabaseForTesting(db *Database) {
	s.dbCacheMu.Lock()
	defer s.dbCacheMu.Unlock()
	s.dbCache = db
}

// datePattern matches date suffixes like -20251101, -2024-11-20, etc.
var datePattern = regexp.MustCompile(`-\d{4}-?\d{2}-?\d{2}$`)

// ResolveModelAlias resolves a model alias to its pinned version.
// For example, ("anthropic", "claude-sonnet-4-5") might resolve to "claude-sonnet-4-5-20250929".
// If the model is not an alias (already pinned or unknown), the original model name is returned.
// This method uses the models.dev database to find the corresponding pinned version.
func (s *Store) ResolveModelAlias(ctx context.Context, providerID, modelName string) string {
	if providerID == "" || modelName == "" {
		return modelName
	}

	// Check if there's a manual alias mapping first
	fullID := providerID + "/" + modelName
	if resolved, ok := ModelAliases[fullID]; ok {
		if _, m, ok := strings.Cut(resolved, "/"); ok {
			return m
		}
		return resolved
	}

	// If the model already has a date suffix, it's already pinned
	if datePattern.MatchString(modelName) {
		return modelName
	}

	// Get the provider from the database
	provider, err := s.GetProvider(ctx, providerID)
	if err != nil {
		return modelName
	}

	// Check if the model exists and is marked as "(latest)"
	model, exists := provider.Models[modelName]
	if !exists || !strings.Contains(model.Name, "(latest)") {
		return modelName
	}

	// Find the pinned version by matching the base display name
	// e.g., "Claude Sonnet 4 (latest)" -> "Claude Sonnet 4"
	baseDisplayName := strings.TrimSuffix(model.Name, " (latest)")

	for pinnedID, pinnedModel := range provider.Models {
		if pinnedID != modelName &&
			!strings.Contains(pinnedModel.Name, "(latest)") &&
			pinnedModel.Name == baseDisplayName &&
			datePattern.MatchString(pinnedID) {
			return pinnedID
		}
	}

	return modelName
}

// bedrockRegionPrefixes contains known regional/inference profile prefixes used in Bedrock model IDs.
// These prefixes should be stripped when looking up models in the database since models.dev
// stores models without regional prefixes. AWS uses these for cross-region inference profiles.
// See: https://docs.aws.amazon.com/bedrock/latest/userguide/cross-region-inference.html
var bedrockRegionPrefixes = map[string]bool{
	"us":     true, // US region inference profile
	"eu":     true, // EU region inference profile
	"apac":   true, // Asia Pacific region inference profile
	"global": true, // Global inference profile (routes to any available region)
}

// isBedrockRegionPrefix returns true if the prefix is a known Bedrock regional/inference profile prefix.
func isBedrockRegionPrefix(prefix string) bool {
	return bedrockRegionPrefixes[prefix]
}

// ModelSupportsReasoning checks if the given model ID supports reasoning/thinking.
//
// This function implements fail-open semantics:
//   - If modelID is empty or not in "provider/model" format, returns true (fail-open)
//   - If models.dev lookup fails for any reason, returns true (fail-open)
//   - If lookup succeeds, returns the model's Reasoning field value
func ModelSupportsReasoning(ctx context.Context, modelID string) bool {
	// Fail-open for empty model ID
	if modelID == "" {
		return true
	}

	// Fail-open if not in provider/model format
	if !strings.Contains(modelID, "/") {
		slog.Debug("Model ID not in provider/model format, assuming reasoning supported to allow user choice", "model_id", modelID)
		return true
	}

	store, err := NewStore()
	if err != nil {
		slog.Debug("Failed to create modelsdev store, assuming reasoning supported to allow user choice", "error", err)
		return true
	}

	model, err := store.GetModel(ctx, modelID)
	if err != nil {
		slog.Debug("Failed to lookup model in models.dev, assuming reasoning supported to allow user choice", "model_id", modelID, "error", err)
		return true
	}

	return model.Reasoning
}
