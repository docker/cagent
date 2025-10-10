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
	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents a Gemini client wrapper
// It implements the provider.Provider interface
type Client struct {
	client       *genai.Client
	config       *latest.ModelConfig
	modelOptions options.ModelOptions
	// When using the Docker AI Gateway, tokens are short-lived. We rebuild
	// the client per request when in gateway mode.
	useGateway     bool
	gatewayBaseURL string
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

	var apiKey string
	var httpOptions genai.HTTPOptions
	useGateway := false
	gatewayBaseURL := ""
	if gateway := modelOptions.Gateway(); gateway == "" {
		apiKey = env.Get(ctx, "GOOGLE_API_KEY")
		if apiKey == "" {
			return nil, errors.New("GOOGLE_API_KEY environment variable is required")
		}
	} else {
		// genai client requires a non-empty API key
		apiKey = desktop.GetToken(ctx)
		if apiKey == "" {
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}
		httpOptions.BaseURL = gateway
		httpOptions.Headers = make(http.Header)
		httpOptions.Headers.Set("Authorization", "Bearer "+apiKey)
		useGateway = true
		gatewayBaseURL = gateway
	}

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      apiKey,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: httpOptions,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		client:         client,
		config:         cfg,
		useGateway:     useGateway,
		gatewayBaseURL: gatewayBaseURL,
		modelOptions:   modelOptions,
	}, nil
}

// newGatewayClient builds a new Gemini client using a fresh Docker Desktop token.
func (c *Client) newGatewayClient(ctx context.Context) (*genai.Client, error) {
	token := desktop.GetToken(ctx)
	if token == "" {
		return nil, errors.New("failed to get Docker Desktop token for gateway")
	}
	httpOptions := genai.HTTPOptions{
		BaseURL: c.gatewayBaseURL,
		Headers: make(http.Header),
	}
	httpOptions.Headers.Set("Authorization", "Bearer "+token)
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      token,
		Backend:     genai.BackendGeminiAPI,
		HTTPOptions: httpOptions,
	})
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
	if c.config == nil {
		return nil
	}

	config := &genai.GenerateContentConfig{
		Temperature:      genai.Ptr(float32(c.config.Temperature)),
		TopP:             genai.Ptr(float32(c.config.TopP)),
		FrequencyPenalty: genai.Ptr(float32(c.config.FrequencyPenalty)),
		PresencePenalty:  genai.Ptr(float32(c.config.PresencePenalty)),
	}
	if c.config.MaxTokens > 0 {
		config.MaxOutputTokens = int32(c.config.MaxTokens)
	}

	// Apply thinking budget for Gemini models using token-based configuration.
	// Per official docs: https://ai.google.dev/gemini-api/docs/thinking
	// - Set thinkingBudget to 0 to disable thinking
	// - Set thinkingBudget to -1 for dynamic thinking (model decides)
	// - Set to a specific value for a fixed token budget,
	//   maximum is 24576 for all models except Gemini 2.5 Pro (max 32768)
	if c.config.ThinkingBudget != nil {
		if config.ThinkingConfig == nil {
			config.ThinkingConfig = &genai.ThinkingConfig{}
		}
		config.ThinkingConfig.IncludeThoughts = true
		tokens := c.config.ThinkingBudget.Tokens
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

	if c.modelOptions.StructuredOutput != nil {
		config.ResponseMIMEType = "application/json"
		config.ResponseJsonSchema = c.modelOptions.StructuredOutput.Schema
	}

	return config
}

// convertToolsToGemini converts tools to Gemini format
func convertToolsToGemini(requestTools []tools.Tool) []*genai.Tool {
	if len(requestTools) == 0 {
		return nil
	}

	funcs := make([]*genai.FunctionDeclaration, 0, len(requestTools))
	for _, tool := range requestTools {
		funcs = append(funcs, &genai.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  ConvertParametersToSchema(tool.Parameters),
		})
	}

	return []*genai.Tool{{
		FunctionDeclarations: funcs,
	}}
}

// ConvertParametersToSchema converts parameters to Gemini Schema format
func ConvertParametersToSchema(params any) *genai.Schema {
	if params == nil {
		return nil
	}

	// Convert FunctionParameters to Schema
	if funcParams, ok := params.(tools.FunctionParameters); ok {
		// Convert type string to Gemini Type
		var schemaType genai.Type
		switch funcParams.Type {
		case "object":
			schemaType = genai.TypeObject
		case "string":
			schemaType = genai.TypeString
		case "number":
			schemaType = genai.TypeNumber
		case "integer":
			schemaType = genai.TypeInteger
		case "boolean":
			schemaType = genai.TypeBoolean
		case "array":
			schemaType = genai.TypeArray
		default:
			schemaType = genai.TypeObject
		}

		schema := &genai.Schema{
			Type:     schemaType,
			Required: funcParams.Required,
		}

		// Convert properties map
		if len(funcParams.Properties) > 0 {
			schema.Properties = make(map[string]*genai.Schema)
			for name := range funcParams.Properties {
				// Parse each property schema
				if propMap, ok := funcParams.Properties[name].(map[string]any); ok {
					propSchema := &genai.Schema{}
					if propType, ok := propMap["type"].(string); ok {
						switch propType {
						case "string":
							propSchema.Type = genai.TypeString
						case "number":
							propSchema.Type = genai.TypeNumber
						case "integer":
							propSchema.Type = genai.TypeInteger
						case "boolean":
							propSchema.Type = genai.TypeBoolean
						case "array":
							propSchema.Type = genai.TypeArray
							propSchema.Items = &genai.Schema{
								Type: genai.TypeString,
							}
						case "object":
							propSchema.Type = genai.TypeObject
						default:
							propSchema.Type = genai.TypeString
						}
					}
					if propDesc, ok := propMap["description"].(string); ok {
						propSchema.Description = propDesc
					}
					schema.Properties[name] = propSchema
				} else {
					// Default to string type
					schema.Properties[name] = &genai.Schema{
						Type: genai.TypeString,
					}
				}
			}
		}

		return schema
	}

	// Fallback for other parameter types
	return &genai.Schema{
		Type: genai.TypeObject,
	}
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
		config.Tools = convertToolsToGemini(requestTools)

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

	// Build a fresh client per request when using the gateway
	var iter func(func(*genai.GenerateContentResponse, error) bool)
	if c.useGateway {
		if gwClient, err := c.newGatewayClient(ctx); err == nil {
			iter = gwClient.Models.GenerateContentStream(ctx, c.config.Model, contents, config)
		} else {
			iter = c.client.Models.GenerateContentStream(ctx, c.config.Model, contents, config)
		}
	} else {
		iter = c.client.Models.GenerateContentStream(ctx, c.config.Model, contents, config)
	}
	return NewStreamAdapter(iter, c.config.Model), nil
}

func (c *Client) ID() string {
	return c.config.Provider + "/" + c.config.Model
}

// Options returns the effective model options used by this client.
func (c *Client) Options() options.ModelOptions {
	return c.modelOptions
}
