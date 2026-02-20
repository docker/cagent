package modelsdev

import (
	"context"
	"encoding/json"
	"fmt"
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
	refreshInterval = 24 * time.Hour
)

// Store manages access to the models.dev data.
// All methods are safe for concurrent use.
type Store struct {
	cacheFile string
	mu        sync.Mutex
	db        *Database
}

// NewStore creates a new models.dev store.
// The database is loaded on first access via GetDatabase.
func NewStore() (*Store, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cagent")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Store{
		cacheFile: filepath.Join(cacheDir, CacheFileName),
	}, nil
}

// NewDatabaseStore creates a Store pre-populated with the given database.
// The returned store serves data entirely from memory and never fetches
// from the network or touches the filesystem, making it suitable for
// tests and any scenario where the provider data is already known.
func NewDatabaseStore(db *Database) *Store {
	return &Store{db: db}
}

// GetDatabase returns the models.dev database, fetching from cache or API as needed.
func (s *Store) GetDatabase(ctx context.Context) (*Database, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		return s.db, nil
	}

	db, err := loadDatabase(ctx, s.cacheFile)
	if err != nil {
		return nil, err
	}

	s.db = db
	return db, nil
}

// GetProvider returns a specific provider by ID.
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

// GetModel returns a specific model by provider ID and model ID.
func (s *Store) GetModel(ctx context.Context, id string) (*Model, error) {
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
			if before, after, ok := strings.Cut(modelID, "."); ok {
				possibleRegionPrefix := before
				if isBedrockRegionPrefix(possibleRegionPrefix) {
					normalizedModelID := after
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

// loadDatabase loads the database from the local cache file or
// falls back to fetching from the models.dev API.
func loadDatabase(ctx context.Context, cacheFile string) (*Database, error) {
	// Try to load from cache first
	cached, err := loadFromCache(cacheFile)
	if err == nil && time.Since(cached.LastRefresh) < refreshInterval {
		return &cached.Database, nil
	}

	// Cache is invalid or doesn't exist, fetch from API
	database, fetchErr := fetchFromAPI(ctx)
	if fetchErr != nil {
		// If API fetch fails, but we have cached data, use it
		if cached != nil {
			return &cached.Database, nil
		}
		return nil, fmt.Errorf("failed to fetch from API and no cached data available: %w", fetchErr)
	}

	// Save to cache
	if err := saveToCache(cacheFile, database); err != nil {
		// Log the error but don't fail the request
		slog.Warn("Warning: failed to save to cache", "error", err)
	}

	return database, nil
}

func fetchFromAPI(ctx context.Context) (*Database, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelsDevAPIURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var providers map[string]Provider
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &Database{
		Providers: providers,
		UpdatedAt: time.Now(),
	}, nil
}

func loadFromCache(cacheFile string) (*CachedData, error) {
	f, err := os.Open(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache file: %w", err)
	}
	defer f.Close()

	var cached CachedData
	if err := json.NewDecoder(f).Decode(&cached); err != nil {
		return nil, fmt.Errorf("failed to decode cached data: %w", err)
	}

	return &cached, nil
}

func saveToCache(cacheFile string, database *Database) error {
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
