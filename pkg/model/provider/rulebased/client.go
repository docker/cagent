// Package rulebased provides a rule-based model router that selects
// the appropriate model based on NLP analysis of the input using Bleve.
//
// Routes are defined with example texts, and Bleve's full-text search
// determines the best matching route based on text similarity.
//
// A model becomes a rule-based router when it has routing rules configured.
// The model's provider/model fields define the fallback model, and each
// routing rule maps example phrases to different target models.
package rulebased

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Provider defines the minimal interface needed for model providers.
type Provider interface {
	ID() string
	CreateChatCompletionStream(
		ctx context.Context,
		messages []chat.Message,
		availableTools []tools.Tool,
	) (chat.MessageStream, error)
	BaseConfig() base.Config
}

// ProviderFactory creates a provider from a model config.
// The models parameter provides access to all configured models for resolving references.
type ProviderFactory func(ctx context.Context, modelSpec string, models map[string]latest.ModelConfig, env environment.Provider, opts ...options.Opt) (Provider, error)

// Client implements the Provider interface for rule-based model routing.
type Client struct {
	base.Config
	routes   []route
	fallback Provider
	index    bleve.Index
}

// route represents a single routing rule.
type route struct {
	model    string
	provider Provider
}

// NewClient creates a new rule-based routing client.
// The cfg parameter should have Routing rules configured. The provider/model
// fields of cfg define the fallback model that is used when no routing rule matches.
func NewClient(ctx context.Context, cfg *latest.ModelConfig, models map[string]latest.ModelConfig, env environment.Provider, providerFactory ProviderFactory, opts ...options.Opt) (*Client, error) {
	slog.Debug("Creating rule-based router", "provider", cfg.Provider, "model", cfg.Model)

	if len(cfg.Routing) == 0 {
		return nil, fmt.Errorf("no routing rules configured")
	}

	index, err := createIndex()
	if err != nil {
		return nil, fmt.Errorf("creating bleve index: %w", err)
	}

	// Create fallback provider from the model's provider/model fields
	fallbackSpec := cfg.Provider + "/" + cfg.Model
	fallback, err := providerFactory(ctx, fallbackSpec, models, env, filterOutMaxTokens(opts)...)
	if err != nil {
		_ = index.Close()
		return nil, fmt.Errorf("creating fallback provider %q: %w", fallbackSpec, err)
	}

	client := &Client{
		Config: base.Config{
			ModelConfig: *cfg,
			Models:      models,
			Env:         env,
		},
		index:    index,
		fallback: fallback,
	}

	// Process routing rules
	for i, rule := range cfg.Routing {
		if rule.Model == "" {
			_ = index.Close()
			return nil, fmt.Errorf("routing rule %d: 'model' field is required", i)
		}

		provider, err := providerFactory(ctx, rule.Model, models, env, filterOutMaxTokens(opts)...)
		if err != nil {
			_ = index.Close()
			return nil, fmt.Errorf("creating provider for routing rule %q: %w", rule.Model, err)
		}

		routeIndex := len(client.routes)
		client.routes = append(client.routes, route{model: rule.Model, provider: provider})

		// Index examples for this route
		for j, example := range rule.Examples {
			docID := fmt.Sprintf("r%d_e%d", routeIndex, j)
			if err := index.Index(docID, map[string]any{"text": example, "route": routeIndex}); err != nil {
				_ = index.Close()
				return nil, fmt.Errorf("indexing example: %w", err)
			}
		}
	}

	return client, nil
}

// createIndex creates an in-memory Bleve index for example matching.
func createIndex() (bleve.Index, error) {
	indexMapping := mapping.NewIndexMapping()

	docMapping := mapping.NewDocumentMapping()
	textField := mapping.NewTextFieldMapping()
	textField.Analyzer = "en"
	docMapping.AddFieldMappingsAt("text", textField)
	docMapping.AddFieldMappingsAt("route", mapping.NewNumericFieldMapping())

	indexMapping.DefaultMapping = docMapping

	return bleve.NewMemOnly(indexMapping)
}

// filterOutMaxTokens removes WithMaxTokens options from the slice.
// This is necessary because child providers may have different token limits
// than the parent router, and should determine their own limits.
func filterOutMaxTokens(opts []options.Opt) []options.Opt {
	var filtered []options.Opt
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		// Test if this option sets maxTokens by applying it to an empty ModelOptions
		var test options.ModelOptions
		opt(&test)
		// If maxTokens was set, skip this option
		if test.MaxTokens() != 0 {
			continue
		}
		filtered = append(filtered, opt)
	}
	return filtered
}

// ID returns the provider identifier.
func (c *Client) ID() string {
	return c.fallback.ID()
}

// CreateChatCompletionStream selects a provider based on input and delegates the call.
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	availableTools []tools.Tool,
) (chat.MessageStream, error) {
	provider := c.selectProvider(messages)
	if provider == nil {
		return nil, fmt.Errorf("no provider available for routing")
	}

	slog.Debug("Rule-based router selected model",
		"router", c.ID(),
		"selected_model", provider.ID(),
		"message_count", len(messages),
	)

	return provider.CreateChatCompletionStream(ctx, messages, availableTools)
}

// selectProvider finds the best matching provider for the messages.
func (c *Client) selectProvider(messages []chat.Message) Provider {
	userMessage := getLastUserMessage(messages)
	if userMessage == "" {
		return c.defaultProvider()
	}

	query := bleve.NewMatchQuery(userMessage)
	query.SetField("text")

	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = 10
	searchRequest.Fields = []string{"route"}

	results, err := c.index.Search(searchRequest)
	if err != nil {
		slog.Error("Bleve search failed", "error", err)
		return c.defaultProvider()
	}

	if results.Total == 0 {
		return c.defaultProvider()
	}

	// Find best matching route by aggregating scores
	scores := make(map[int]float64)
	for _, hit := range results.Hits {
		var routeIdx int
		if _, err := fmt.Sscanf(hit.ID, "r%d_e", &routeIdx); err == nil {
			if hit.Score > scores[routeIdx] {
				scores[routeIdx] = hit.Score
			}
		}
	}

	bestRoute, bestScore := -1, 0.0
	for idx, score := range scores {
		if score > bestScore {
			bestRoute, bestScore = idx, score
		}
	}

	if bestRoute >= 0 && bestRoute < len(c.routes) {
		slog.Debug("Route matched",
			"model", c.routes[bestRoute].model,
			"score", bestScore,
		)
		return c.routes[bestRoute].provider
	}

	return c.defaultProvider()
}

func (c *Client) defaultProvider() Provider {
	if c.fallback != nil {
		return c.fallback
	}
	if len(c.routes) > 0 {
		return c.routes[0].provider
	}
	return nil
}

func getLastUserMessage(messages []chat.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == chat.MessageRoleUser {
			return messages[i].Content
		}
	}
	return ""
}

// BaseConfig returns the base configuration.
func (c *Client) BaseConfig() base.Config {
	return c.Config
}

// Close cleans up resources.
func (c *Client) Close() error {
	if c.index != nil {
		return c.index.Close()
	}
	return nil
}
