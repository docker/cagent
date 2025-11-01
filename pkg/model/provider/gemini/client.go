package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
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

	var modelOptions options.ModelOptions
	for _, opt := range opts {
		opt(&modelOptions)
	}

	var clientFn func(context.Context) (*genai.Client, error)
	if gateway := modelOptions.Gateway(); gateway == "" {
		apiKey := env.Get(ctx, "GOOGLE_API_KEY")
		if apiKey == "" {
			return nil, errors.New("GOOGLE_API_KEY environment variable is required")
		}

		client, err := genai.NewClient(ctx, &genai.ClientConfig{
			APIKey:     apiKey,
			Backend:    genai.BackendGeminiAPI,
			HTTPClient: httpclient.NewHTTPClient(),
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

			return genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:  authToken,
				Backend: genai.BackendGeminiAPI,
				HTTPClient: httpclient.NewHTTPClient(
					httpclient.WithProxiedBaseURL(defaultsTo(cfg.BaseURL, "https://generativelanguage.googleapis.com/")),
					httpclient.WithProvider(cfg.Provider),
					httpclient.WithModel(cfg.Model),
				),
				HTTPOptions: genai.HTTPOptions{
					BaseURL: gateway,
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
			ModelConfig:  cfg,
			ModelOptions: modelOptions,
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
				parts = append(parts, genai.NewPartFromText(msg.Content))
			}

			// Add function calls
			for _, tc := range msg.ToolCalls {
				// Parse arguments from JSON string to map
				var args map[string]any
				if tc.Function.Arguments != "" {
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
				}

				parts = append(parts, genai.NewPartFromFunctionCall(tc.Function.Name, args))
			}

			contents = append(contents, genai.NewContentFromParts(parts, role))
			continue
		}

		// Handle regular messages
		if len(msg.MultiContent) > 0 {
			parts := make([]*genai.Part, 0, len(msg.MultiContent))
			for _, part := range msg.MultiContent {
				if part.Type == chat.MessagePartTypeText {
					parts = append(parts, genai.NewPartFromText(part.Text))
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
			contents = append(contents, genai.NewContentFromText(msg.Content, role))
		}
	}
	return contents
}

// buildConfig creates GenerateContentConfig from model config
func (c *Client) buildConfig() *genai.GenerateContentConfig {
	if c.ModelConfig == nil {
		return nil
	}

	config := &genai.GenerateContentConfig{
		Temperature:      genai.Ptr(float32(c.ModelConfig.Temperature)),
		TopP:             genai.Ptr(float32(c.ModelConfig.TopP)),
		FrequencyPenalty: genai.Ptr(float32(c.ModelConfig.FrequencyPenalty)),
		PresencePenalty:  genai.Ptr(float32(c.ModelConfig.PresencePenalty)),
	}
	if c.ModelConfig.MaxTokens > 0 {
		config.MaxOutputTokens = int32(c.ModelConfig.MaxTokens)
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
	return NewStreamAdapter(iter, c.ModelConfig.Model), nil
}

func defaultsTo(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}
