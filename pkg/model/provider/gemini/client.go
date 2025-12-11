package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/genai"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/rag/prompts"
	"github.com/docker/cagent/pkg/rag/types"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents a Gemini client wrapper
// It implements the provider.Provider interface
type Client struct {
	base.Config
	clientFn func(context.Context) (*genai.Client, error)
}

// NewClient creates a new Gemini client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "google" {
		return nil, errors.New("model type must be 'google'")
	}

	// Ensure env is not nil - use default provider if nil
	if env == nil {
		env = environment.NewDefaultProvider()
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	var clientFn func(context.Context) (*genai.Client, error)
	if gateway := globalOptions.Gateway(); gateway == "" {
		var (
			httpClient *http.Client
			backend    genai.Backend
			apiKey     string
			project    string
			location   string
		)
		// project/location take priority over API key, like in the genai client.
		if cfg.ProviderOpts["project"] != nil || cfg.ProviderOpts["location"] != nil {
			var err error

			project, err = environment.Expand(ctx, providerOption(cfg, "project"), env)
			if err != nil {
				return nil, fmt.Errorf("expanding project: %w", err)
			}
			if project == "" {
				return nil, errors.New("project must be set")
			}

			location, err = environment.Expand(ctx, providerOption(cfg, "location"), env)
			if err != nil {
				return nil, fmt.Errorf("expanding location: %w", err)
			}
			if location == "" {
				return nil, errors.New("location must be set")
			}

			backend = genai.BackendVertexAI
			httpClient = nil // Use default client
		} else {
			apiKey = env.Get(ctx, "GOOGLE_API_KEY")
			if apiKey == "" {
				return nil, errors.New("GOOGLE_API_KEY environment variable is required")
			}

			backend = genai.BackendGeminiAPI
			httpClient = httpclient.NewHTTPClient()
		}

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     apiKey,
			Project:    project,
			Location:   location,
			Backend:    backend,
			HTTPClient: httpClient,
			HTTPOptions: genai.HTTPOptions{
				BaseURL: cfg.BaseURL,
			},
		})
		if err != nil {
			return nil, err
		}

		clientFn = func(context.Context) (*genai.Client, error) {
			return client, nil
		}
	} else {
		// Fail fast if Docker Desktop's auth token isn't available
		if env.Get(ctx, environment.DockerDesktopTokenEnv) == "" {
			slog.Error("Gemini client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		// When using a Gateway, tokens are short-lived.
		clientFn = func(ctx context.Context) (*genai.Client, error) {
			// Query a fresh auth token each time the client is used
			authToken := env.Get(ctx, environment.DockerDesktopTokenEnv)
			if authToken == "" {
				return nil, errors.New("failed to get Docker Desktop token for Gateway")
			}

			url, err := url.Parse(gateway)
			if err != nil {
				return nil, fmt.Errorf("invalid gateway URL: %w", err)
			}
			baseURL := fmt.Sprintf("%s://%s%s/", url.Scheme, url.Host, url.Path)

			httpOptions := []httpclient.Opt{
				httpclient.WithProxiedBaseURL(defaultsTo(cfg.BaseURL, "https://generativelanguage.googleapis.com/")),
				httpclient.WithProvider(cfg.Provider),
				httpclient.WithModel(cfg.Model),
				httpclient.WithQuery(url.Query()),
			}
			if globalOptions.GeneratingTitle() {
				httpOptions = append(httpOptions, httpclient.WithHeader("X-Cagent-GeneratingTitle", "1"))
			}

			return genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:     authToken,
				Backend:    genai.BackendGeminiAPI,
				HTTPClient: httpclient.NewHTTPClient(httpOptions...),
				HTTPOptions: genai.HTTPOptions{
					BaseURL: baseURL,
					Headers: http.Header{
						"Authorization": []string{"Bearer " + authToken},
					},
				},
			})
		}
	}

	slog.Debug("Gemini client created successfully", "model", cfg.Model)

	return &Client{
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
			Env:          env,
		},
		clientFn: clientFn,
	}, nil
}

// convertMessagesToGemini converts chat.Messages into Gemini Contents
func convertMessagesToGemini(messages []chat.Message) []*genai.Content {
	contents := make([]*genai.Content, 0, len(messages))
	for i := range messages {
		msg := &messages[i]

		// Skip empty messages
		if msg.Content == "" && len(msg.MultiContent) == 0 && len(msg.ToolCalls) == 0 && msg.ToolCallID == "" {
			continue
		}

		var role genai.Role
		switch msg.Role {
		case chat.MessageRoleAssistant:
			role = genai.RoleModel
		default:
			role = genai.RoleUser
		}

		// Handle tool responses
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID != "" {
			// Create a function response part
			part := genai.NewPartFromFunctionResponse(msg.ToolCallID, map[string]any{
				"result": msg.Content,
			})
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{part}, role))
			continue
		}

		// Handle assistant messages with tool calls
		if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) > 0 {
			parts := make([]*genai.Part, 0)

			// Add text content if present
			if msg.Content != "" {
				contentPart := genai.NewPartFromText(msg.Content)
				// Set ThoughtSignature on the text part if present
				if len(msg.ThoughtSignature) > 0 {
					contentPart.ThoughtSignature = msg.ThoughtSignature
				}
				parts = append(parts, contentPart)
			}

			// Add function calls
			for _, tc := range msg.ToolCalls {
				// Parse arguments from JSON string to map
				var args map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}

				fc := genai.NewPartFromFunctionCall(tc.Function.Name, args)
				// Set ThoughtSignature on function call if present
				if len(msg.ThoughtSignature) > 0 {
					fc.ThoughtSignature = msg.ThoughtSignature
				}
				parts = append(parts, fc)
			}

			contents = append(contents, genai.NewContentFromParts(parts, role))
			continue
		}

		// Handle regular messages
		if len(msg.MultiContent) > 0 {
			parts := make([]*genai.Part, 0, len(msg.MultiContent))
			for _, part := range msg.MultiContent {
				if part.Type == chat.MessagePartTypeText {
					textPart := genai.NewPartFromText(part.Text)
					// Set ThoughtSignature on the text part if present
					if len(msg.ThoughtSignature) > 0 {
						textPart.ThoughtSignature = msg.ThoughtSignature
					}
					parts = append(parts, textPart)
				} else if part.Type == chat.MessagePartTypeImageURL && part.ImageURL != nil {
					// For Gemini, we need to extract base64 data from data URL and convert to bytes
					// Based on: https://ai.google.dev/gemini-api/docs/vision
					if strings.HasPrefix(part.ImageURL.URL, "data:") {
						urlParts := strings.SplitN(part.ImageURL.URL, ",", 2)
						if len(urlParts) == 2 {
							// Extract media type from data URL
							mediaTypePart := urlParts[0]
							base64Data := urlParts[1]

							// Decode base64 data to bytes
							if imageData, err := base64.StdEncoding.DecodeString(base64Data); err == nil {
								var mimeType string
								switch {
								case strings.Contains(mediaTypePart, "image/jpeg"):
									mimeType = "image/jpeg"
								case strings.Contains(mediaTypePart, "image/png"):
									mimeType = "image/png"
								case strings.Contains(mediaTypePart, "image/gif"):
									mimeType = "image/gif"
								case strings.Contains(mediaTypePart, "image/webp"):
									mimeType = "image/webp"
								default:
									mimeType = "image/jpeg" // Default
								}

								// Create image part using Gemini Go SDK
								// Equivalent to types.Part.from_bytes(data=image_bytes, mime_type='image/jpeg')
								parts = append(parts, genai.NewPartFromBytes(imageData, mimeType))
							}
						}
					}
				}
			}
			if len(parts) > 0 {
				contents = append(contents, genai.NewContentFromParts(parts, role))
			}
		} else if msg.Content != "" {
			// Create a text part and set ThoughtSignature if present
			contentPart := genai.NewPartFromText(msg.Content)
			if len(msg.ThoughtSignature) > 0 {
				contentPart.ThoughtSignature = msg.ThoughtSignature
			}
			contents = append(contents, genai.NewContentFromParts([]*genai.Part{contentPart}, role))
		}
	}
	return contents
}

// buildConfig creates GenerateContentConfig from model config
func (c *Client) buildConfig() *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{}
	if c.ModelConfig.MaxTokens > 0 {
		config.MaxOutputTokens = int32(c.ModelConfig.MaxTokens)
	}
	if c.ModelConfig.Temperature != nil {
		config.Temperature = genai.Ptr(float32(*c.ModelConfig.Temperature))
	}
	if c.ModelConfig.TopP != nil {
		config.TopP = genai.Ptr(float32(*c.ModelConfig.TopP))
	}
	if c.ModelConfig.FrequencyPenalty != nil {
		config.FrequencyPenalty = genai.Ptr(float32(*c.ModelConfig.FrequencyPenalty))
	}
	if c.ModelConfig.PresencePenalty != nil {
		config.PresencePenalty = genai.Ptr(float32(*c.ModelConfig.PresencePenalty))
	}

	// Apply thinking budget for Gemini models using token-based configuration.
	// Per official docs: https://ai.google.dev/gemini-api/docs/thinking
	// - Set thinkingBudget to 0 to disable thinking
	// - Set thinkingBudget to -1 for dynamic thinking (model decides)
	// - Set to a specific value for a fixed token budget,
	//   maximum is 24576 for all models except Gemini 2.5 Pro (max 32768)
	if c.ModelConfig.ThinkingBudget != nil {
		if config.ThinkingConfig == nil {
			config.ThinkingConfig = &genai.ThinkingConfig{}
		}
		config.ThinkingConfig.IncludeThoughts = true
		tokens := c.ModelConfig.ThinkingBudget.Tokens
		config.ThinkingConfig.ThinkingBudget = genai.Ptr(int32(tokens))

		switch tokens {
		case 0:
			slog.Debug("Gemini request with thinking disabled", "budget_tokens", tokens)
		case -1:
			slog.Debug("Gemini request with dynamic thinking", "budget_tokens", tokens)
		default:
			slog.Debug("Gemini request using thinking_budget", "budget_tokens", tokens)
		}
	}

	if structuredOutput := c.ModelOptions.StructuredOutput(); structuredOutput != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseJsonSchema = structuredOutput.Schema
	}

	return config
}

// convertToolsToGemini converts tools to Gemini format
func convertToolsToGemini(requestTools []tools.Tool) ([]*genai.Tool, error) {
	if len(requestTools) == 0 {
		return nil, nil
	}

	funcs := make([]*genai.FunctionDeclaration, 0, len(requestTools))
	for _, tool := range requestTools {
		parameters, err := ConvertParametersToSchema(tool.Parameters)
		if err != nil {
			return nil, err
		}

		funcs = append(funcs, &genai.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  parameters,
		})
	}

	return []*genai.Tool{{
		FunctionDeclarations: funcs,
	}}, nil
}

// ConvertParametersToSchema converts parameters to Gemini Schema format
func ConvertParametersToSchema(params any) (*genai.Schema, error) {
	var schema *genai.Schema
	if err := tools.ConvertSchema(params, &schema); err != nil {
		return nil, err
	}

	return schema, nil
}

// CreateChatCompletionStream creates a streaming chat completion request
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	if len(messages) == 0 {
		return nil, errors.New("at least one message is required")
	}

	config := c.buildConfig()

	// Add tools to config if provided
	if len(requestTools) > 0 {
		allTools, err := convertToolsToGemini(requestTools)
		if err != nil {
			slog.Error("Failed to convert tools to Gemini format", "error", err)
			return nil, err
		}

		config.Tools = allTools

		// Enable function calling
		config.ToolConfig = &genai.ToolConfig{
			FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAuto,
			},
		}

		// Debug: Log the tools we're sending
		slog.Debug("Gemini tools config", "tools", config.Tools)
		for _, tool := range config.Tools {
			for _, fn := range tool.FunctionDeclarations {
				slog.Debug("Function", "name", fn.Name, "desc", fn.Description, "params", fn.Parameters)
			}
		}
	}

	contents := convertMessagesToGemini(messages)

	// Debug: Log the messages we're sending
	slog.Debug("Gemini messages", "count", len(contents))
	for i, content := range contents {
		slog.Debug("Message", "index", i, "role", content.Role)
	}

	client, err := c.clientFn(ctx)
	if err != nil {
		slog.Error("Failed to create Gemini client", "error", err)
		return nil, err
	}

	// Build a fresh client per request when using the gateway
	iter := client.Models.GenerateContentStream(ctx, c.ModelConfig.Model, contents, config)
	trackUsage := c.ModelConfig.TrackUsage == nil || *c.ModelConfig.TrackUsage
	return NewStreamAdapter(iter, c.ModelConfig.Model, trackUsage), nil
}

// Rerank scores documents by relevance to the query using Gemini's structured
// output feature. It returns relevance scores in the same order as input documents.
func (c *Client) Rerank(ctx context.Context, query string, documents []types.Document, criteria string) ([]float64, error) {
	const logPrefix = "Gemini reranking request"

	if len(documents) == 0 {
		slog.Debug(logPrefix, "model", c.ModelConfig.Model, "num_documents", 0)
		return []float64{}, nil
	}

	slog.Debug(logPrefix,
		"model", c.ModelConfig.Model,
		"query_length", len(query),
		"num_documents", len(documents),
		"has_criteria", criteria != "")

	client, err := c.clientFn(ctx)
	if err != nil {
		slog.Error("Failed to create Gemini client for reranking", "error", err)
		return nil, err
	}

	// Build user prompt with query and numbered documents (including metadata)
	userPrompt := prompts.BuildRerankDocumentsPrompt(query, documents)

	// Build system prompt with Gemini-specific JSON format instructions
	jsonFormatInstruction := `Return a JSON object with a "scores" array containing one number per document, in order.`
	systemPrompt := prompts.BuildRerankSystemPrompt(documents, criteria, c.ModelConfig.ProviderOpts, jsonFormatInstruction)

	// Create a single user turn that includes both system-like instruction and data.
	content := genai.NewContentFromParts(
		[]*genai.Part{
			genai.NewPartFromText(systemPrompt),
			genai.NewPartFromText(userPrompt),
		},
		genai.RoleUser,
	)

	// Use Gemini's structured output feature to enforce the JSON schema.
	// This eliminates the need for fallback parsing strategies.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"scores": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "number",
				},
			},
		},
		"required": []string{"scores"},
	}

	// Start with standard config from model definition (respects max_tokens,
	// temperature, top_p, etc. from the model config).
	// If the user hasn't configured these, we rely on Gemini API defaults.
	cfg := c.buildConfig()

	// Override with reranking-specific structured output schema.
	cfg.ResponseMIMEType = "application/json"
	cfg.ResponseJsonSchema = schema

	// For reranking, default temperature to 0 for deterministic scoring if not explicitly set.
	if c.ModelConfig.Temperature == nil {
		cfg.Temperature = genai.Ptr(float32(0.0))
	}

	// Disable thinking for reranking - we want quick, deterministic scoring
	// without wasting tokens on internal reasoning. This overrides any
	// thinking_budget from the model config for this specific use case.
	cfg.ThinkingConfig = &genai.ThinkingConfig{
		IncludeThoughts: false,
	}

	resp, err := client.Models.GenerateContent(ctx, c.ModelConfig.Model, []*genai.Content{content}, cfg)
	if err != nil {
		slog.Error("Gemini rerank request failed", "error", err)
		return nil, fmt.Errorf("gemini rerank request failed: %w", err)
	}

	// Check if the request was blocked by safety filters
	if resp.PromptFeedback != nil && resp.PromptFeedback.BlockReason != "" {
		return nil, fmt.Errorf("gemini blocked request: %v", resp.PromptFeedback.BlockReason)
	}

	rawJSON, err := extractGeminiStructuredJSON(resp)
	if err != nil {
		slog.Error("Failed to extract Gemini structured JSON", "error", err)
		return nil, err
	}

	scores, err := parseRerankScoresStrict(rawJSON, len(documents))
	if err != nil {
		slog.Error("Failed to parse Gemini rerank scores", "error", err)
		return nil, err
	}

	slog.Debug("Gemini reranking complete",
		"model", c.ModelConfig.Model,
		"num_scores", len(scores))

	return scores, nil
}

// extractGeminiStructuredJSON extracts the JSON string from a
// GenerateContentResponse with structured output enabled.
func extractGeminiStructuredJSON(resp *genai.GenerateContentResponse) (string, error) {
	if resp == nil {
		return "", errors.New("gemini response is nil")
	}

	if len(resp.Candidates) == 0 {
		return "", errors.New("gemini response has no candidates")
	}

	for _, cand := range resp.Candidates {
		if cand == nil || cand.Content == nil {
			continue
		}

		for _, part := range cand.Content.Parts {
			if part != nil && part.Text != "" {
				return part.Text, nil
			}
		}
	}

	return "", errors.New("no text part found in Gemini response for structured JSON")
}

// parseRerankScoresStrict parses a JSON payload of the form {"scores":[...]}
// and validates length. This version does NOT have fallback parsing since
// structured outputs guarantee valid JSON.
func parseRerankScoresStrict(raw string, expected int) ([]float64, error) {
	type rerankResponse struct {
		Scores []float64 `json:"scores"`
	}

	var rr rerankResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &rr); err != nil {
		return nil, fmt.Errorf("failed to parse rerank JSON: %w", err)
	}

	if len(rr.Scores) != expected {
		return nil, fmt.Errorf("expected %d scores, got %d", expected, len(rr.Scores))
	}

	return rr.Scores, nil
}

func defaultsTo(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

func providerOption(cfg *latest.ModelConfig, name string) string {
	v := cfg.ProviderOpts[name]
	if v, ok := v.(string); ok {
		return v
	}
	return ""
}
