package oca

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/cagent/pkg/paths"
)

const modelsCacheFile = "oca_models.json"
const modelsCacheTTL = 24 * time.Hour

// ModelInfo represents an OCA model from the litellm endpoint.
type ModelInfo struct {
	ID      string `json:"id"`
	OwnedBy string `json:"owned_by,omitempty"`
}

type modelsCache struct {
	Models    []ModelInfo `json:"models"`
	FetchedAt int64       `json:"fetched_at"`
}

// FetchModels retrieves the list of available models from the litellm endpoint.
func FetchModels(ctx context.Context, baseURL, token string) ([]ModelInfo, error) {
	modelsURL := baseURL + "models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cagent-oca/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading models response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models request failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing models response: %w", err)
	}

	return result.Data, nil
}

// GetCachedModels returns models from cache if fresh, otherwise fetches and caches.
func GetCachedModels(ctx context.Context, baseURL, token string) ([]ModelInfo, error) {
	cachePath := filepath.Join(paths.GetDataDir(), modelsCacheFile)

	// Try to load from cache
	if data, err := os.ReadFile(cachePath); err == nil {
		var cache modelsCache
		if err := json.Unmarshal(data, &cache); err == nil {
			age := time.Since(time.Unix(cache.FetchedAt, 0))
			if age < modelsCacheTTL && len(cache.Models) > 0 {
				return cache.Models, nil
			}
		}
	}

	// Fetch fresh
	models, err := FetchModels(ctx, baseURL, token)
	if err != nil {
		return nil, err
	}

	// Save to cache (best effort)
	cache := modelsCache{
		Models:    models,
		FetchedAt: time.Now().Unix(),
	}
	if data, err := json.MarshalIndent(cache, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o700)
		_ = os.WriteFile(cachePath, data, 0o600)
	}

	return models, nil
}
