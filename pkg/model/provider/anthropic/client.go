package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents an Anthropic client wrapper implementing provider.Provider
// It holds the anthropic client and model config
type Client struct {
	client anthropic.Client
	config *latest.ModelConfig
	// When using the Docker AI Gateway, tokens are short-lived. We rebuild
	// the client per request when in gateway mode.
	useGateway     bool
	gatewayBaseURL string
}

// interleavedThinkingEnabled returns false unless explicitly enabled via
// models:provider_opts:interleaved_thinking: true
func (c *Client) interleavedThinkingEnabled() bool {
	// Default to false if not provided
	if c == nil || c.config == nil || len(c.config.ProviderOpts) == 0 {
		return false
	}
	v, ok := c.config.ProviderOpts["interleaved_thinking"]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s != "false" && s != "0" && s != "no"
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}

// NewClient creates a new Anthropic client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("Anthropic client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "anthropic" {
		slog.Error("Anthropic client creation failed", "error", "model type must be 'anthropic'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'anthropic'")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	var requestOptions []option.RequestOption
	useGateway := false
	gatewayBaseURL := ""
	if gateway := globalOptions.Gateway(); gateway == "" {
		authToken := env.Get(ctx, "ANTHROPIC_API_KEY")
		if authToken == "" {
			return nil, errors.New("ANTHROPIC_API_KEY environment variable is required")
		}

		slog.Debug("Anthropic API key found, creating client")
		requestOptions = append(requestOptions,
			option.WithAPIKey(authToken),
		)
	} else {
		authToken := desktop.GetToken(ctx)
		if authToken == "" {
			slog.Error("Anthropic client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		slog.Debug("Docker Desktop's authentication token found, creating client")
		requestOptions = append(requestOptions,
			option.WithAuthToken(authToken),
			option.WithAPIKey(authToken),
			option.WithBaseURL(gateway),
		)
		useGateway = true
		gatewayBaseURL = gateway
	}

	client := anthropic.NewClient(requestOptions...)
	slog.Debug("Anthropic client created successfully", "model", cfg.Model)

	return &Client{
		client:         client,
		config:         cfg,
		useGateway:     useGateway,
		gatewayBaseURL: gatewayBaseURL,
	}, nil
}

// newGatewayClient builds a new Anthropic client using a fresh Docker Desktop token.
func (c *Client) newGatewayClient(ctx context.Context) anthropic.Client {
	authToken := desktop.GetToken(ctx)
	opts := []option.RequestOption{
		option.WithAuthToken(authToken),
		option.WithAPIKey(authToken),
	}
	if c.gatewayBaseURL != "" {
		opts = append(opts, option.WithBaseURL(c.gatewayBaseURL))
	}
	return anthropic.NewClient(opts...)
}

// CreateChatCompletionStream creates a streaming chat completion request
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating Anthropic chat completion stream",
		"model", c.config.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	maxTokens := int64(c.config.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 8192 // min output limit for anthropic models >= 3.5
	}

	// Build a fresh client per request when using the gateway
	client := c.client
	if c.useGateway {
		client = c.newGatewayClient(ctx)
	}

	// Use Beta API with interleaved thinking only when enabled
	if c.interleavedThinkingEnabled() {
		return c.createBetaStream(ctx, client, messages, requestTools, maxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.config.Model),
		MaxTokens: maxTokens,
		Messages:  convertMessages(messages),
		Tools:     convertTools(requestTools),
	}

	// Populate proper Anthropic system prompt from input messages
	if sys := extractSystemBlocks(messages); len(sys) > 0 {
		params.System = sys
	}

	// Apply thinking budget
	if c.config.ThinkingBudget != nil && c.config.ThinkingBudget.Tokens > 0 {
		thinkingTokens := int64(c.config.ThinkingBudget.Tokens)
		switch {
		case thinkingTokens >= 1024 && thinkingTokens < maxTokens:
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(thinkingTokens)
			slog.Debug("Anthropic API using thinking_budget (standard messages)", "budget_tokens", thinkingTokens)
		case thinkingTokens >= maxTokens:
			slog.Warn("Anthropic thinking_budget must be less than max_tokens, ignoring", "tokens", thinkingTokens, "max_tokens", maxTokens)
		default:
			slog.Warn("Anthropic thinking_budget below minimum (1024), ignoring", "tokens", thinkingTokens)
		}
	}

	if len(requestTools) > 0 {
		slog.Debug("Adding tools to Anthropic request", "tool_count", len(requestTools))
	}

	// Log the request details for debugging
	slog.Debug("Anthropic chat completion stream request",
		"model", params.Model,
		"max_tokens", maxTokens,
		"message_count", len(params.Messages))

	if slog.Default().Enabled(ctx, slog.LevelDebug) {
		b, err := json.Marshal(params)
		if err != nil {
			slog.Error("Failed to marshal Anthropic request", "error", err)
		}
		slog.Debug("Request", "request", string(b))
	}

	stream := client.Messages.NewStreaming(ctx, params)
	slog.Debug("Anthropic chat completion stream created successfully", "model", c.config.Model)

	return newStreamAdapter(stream), nil
}

func (c *Client) CreateChatCompletion(
	ctx context.Context,
	messages []chat.Message,
) (string, error) {
	slog.Debug("Creating Anthropic chat completion", "model", c.config.Model, "message_count", len(messages))

	maxTokens := int64(c.config.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 8192 // min output limit for anthropic models >= 3.5
	}

	client := c.client

	// Build a fresh client per request when using the gateway
	if c.useGateway {
		client = c.newGatewayClient(ctx)
	}

	// Use Beta API with interleaved thinking
	if c.interleavedThinkingEnabled() {
		return c.createBetaCompletion(ctx, client, messages, maxTokens)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.config.Model),
		MaxTokens: maxTokens,
		Messages:  convertMessages(messages),
	}

	// Populate proper Anthropic system prompt from input messages
	if sys := extractSystemBlocks(messages); len(sys) > 0 {
		params.System = sys
	}

	if c.config.ThinkingBudget != nil && c.config.ThinkingBudget.Tokens > 0 {
		thinkingTokens := int64(c.config.ThinkingBudget.Tokens)
		switch {
		case thinkingTokens >= 1024 && thinkingTokens < maxTokens:
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(thinkingTokens)
			slog.Debug("Anthropic API using thinking_budget (standard messages)", "budget_tokens", thinkingTokens)
		case thinkingTokens >= maxTokens:
			slog.Warn("Anthropic API thinking_budget must be less than max_tokens, ignoring", "tokens", thinkingTokens, "max_tokens", maxTokens)
		default:
			slog.Warn("Anthropic API thinking_budget below minimum (1024), ignoring", "tokens", thinkingTokens)
		}
	}

	response, err := client.Messages.New(ctx, params)
	if err != nil {
		slog.Error("Anthropic API chat completion failed", "error", err, "model", c.config.Model)
		return "", err
	}

	// Extract text from response content (skip thinking blocks)
	var result strings.Builder
	for i := range response.Content {
		if response.Content[i].Text != "" {
			result.WriteString(response.Content[i].Text)
		}
	}

	slog.Debug("Anthropic API chat completion successful", "model", c.config.Model, "response_length", result.Len())
	return result.String(), nil
}

func convertMessages(messages []chat.Message) []anthropic.MessageParam {
	var anthropicMessages []anthropic.MessageParam

	for i := range messages {
		msg := &messages[i]
		if msg.Role == chat.MessageRoleSystem {
			// System messages are handled via the top-level params.System
			continue
		}
		if msg.Role == chat.MessageRoleUser {
			// Handle MultiContent for user messages (including images)
			if len(msg.MultiContent) > 0 {
				contentBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.MultiContent))
				for _, part := range msg.MultiContent {
					if part.Type == chat.MessagePartTypeText {
						if txt := strings.TrimSpace(part.Text); txt != "" {
							contentBlocks = append(contentBlocks, anthropic.NewTextBlock(txt))
						}
					} else if part.Type == chat.MessagePartTypeImageURL && part.ImageURL != nil {
						// Anthropic expects base64 image data
						// Extract base64 data from data URL
						if strings.HasPrefix(part.ImageURL.URL, "data:") {
							parts := strings.SplitN(part.ImageURL.URL, ",", 2)
							if len(parts) == 2 {
								// Extract media type from data URL
								mediaTypePart := parts[0]
								base64Data := parts[1]

								var mediaType string
								switch {
								case strings.Contains(mediaTypePart, "image/jpeg"):
									mediaType = "image/jpeg"
								case strings.Contains(mediaTypePart, "image/png"):
									mediaType = "image/png"
								case strings.Contains(mediaTypePart, "image/gif"):
									mediaType = "image/gif"
								case strings.Contains(mediaTypePart, "image/webp"):
									mediaType = "image/webp"
								default:
									// Default to jpeg if not recognized
									mediaType = "image/jpeg"
								}

								// Create image block using raw JSON approach
								// Based on: https://docs.anthropic.com/en/api/messages-vision
								imageBlockJSON := map[string]any{
									"type": "image",
									"source": map[string]any{
										"type":       "base64",
										"media_type": mediaType,
										"data":       base64Data,
									},
								}

								// Convert to JSON and back to ContentBlockParamUnion
								jsonBytes, err := json.Marshal(imageBlockJSON)
								if err == nil {
									var imageBlock anthropic.ContentBlockParamUnion
									if json.Unmarshal(jsonBytes, &imageBlock) == nil {
										contentBlocks = append(contentBlocks, imageBlock)
									}
								}
							}
						}
					}
				}
				if len(contentBlocks) > 0 {
					anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(contentBlocks...))
				}
			} else {
				if txt := strings.TrimSpace(msg.Content); txt != "" {
					anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(anthropic.NewTextBlock(txt)))
				}
			}
			continue
		}
		if msg.Role == chat.MessageRoleAssistant {
			contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

			// Include thinking blocks when present to preserve extended thinking context
			if msg.ReasoningContent != "" && msg.ThinkingSignature != "" {
				contentBlocks = append(contentBlocks, anthropic.NewThinkingBlock(msg.ThinkingSignature, msg.ReasoningContent))
			} else if msg.ThinkingSignature != "" {
				contentBlocks = append(contentBlocks, anthropic.NewRedactedThinkingBlock(msg.ThinkingSignature))
			}

			if len(msg.ToolCalls) > 0 {
				blockLen := len(msg.ToolCalls)
				msgContent := strings.TrimSpace(msg.Content)
				offset := 0
				if msgContent != "" {
					blockLen++
				}
				toolUseBlocks := make([]anthropic.ContentBlockParamUnion, blockLen)
				// If there is prior thinking, append it first
				if len(contentBlocks) > 0 {
					toolUseBlocks = append(contentBlocks, toolUseBlocks...)
				}
				if msgContent != "" {
					toolUseBlocks[len(contentBlocks)+offset] = anthropic.NewTextBlock(msgContent)
					offset = 1
				}
				for j, toolCall := range msg.ToolCalls {
					var inpts map[string]any
					if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &inpts); err != nil {
						inpts = map[string]any{}
					}
					toolUseBlocks[len(contentBlocks)+j+offset] = anthropic.ContentBlockParamUnion{
						OfToolUse: &anthropic.ToolUseBlockParam{
							ID:    toolCall.ID,
							Input: inpts,
							Name:  toolCall.Function.Name,
						},
					}
				}
				anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(toolUseBlocks...))
			} else {
				if txt := strings.TrimSpace(msg.Content); txt != "" {
					contentBlocks = append(contentBlocks, anthropic.NewTextBlock(txt))
				}
				if len(contentBlocks) > 0 {
					anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(contentBlocks...))
				}
			}
			continue
		}
		if msg.Role == chat.MessageRoleTool {
			toolResult := anthropic.NewToolResultBlock(msg.ToolCallID, strings.TrimSpace(msg.Content), false)
			anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(toolResult))
			continue
		}
	}
	return anthropicMessages
}

// extractSystemBlocks converts any system-role messages into Anthropic system text blocks
// to be set on the top-level MessageNewParams.System field.
func extractSystemBlocks(messages []chat.Message) []anthropic.TextBlockParam {
	var systemBlocks []anthropic.TextBlockParam
	for i := range messages {
		msg := &messages[i]
		if msg.Role != chat.MessageRoleSystem {
			continue
		}
		if len(msg.MultiContent) > 0 {
			for _, part := range msg.MultiContent {
				if part.Type == chat.MessagePartTypeText {
					if txt := strings.TrimSpace(part.Text); txt != "" {
						systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: txt})
					}
				}
			}
		} else if txt := strings.TrimSpace(msg.Content); txt != "" {
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: txt})
		}
	}
	return systemBlocks
}

func convertTools(tooles []tools.Tool) []anthropic.ToolUnionParam {
	toolParams := make([]anthropic.ToolParam, len(tooles))

	for i, tool := range tooles {
		toolParams[i] = anthropic.ToolParam{
			Name:        tool.Name,
			Description: anthropic.String(tool.Description),
			InputSchema: ConvertParametersToSchema(tool.Parameters),
		}
	}
	anthropicTools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i := range toolParams {
		anthropicTools[i] = anthropic.ToolUnionParam{OfTool: &toolParams[i]}
	}

	return anthropicTools
}

// ConvertParametersToSchema converts parameters to Anthropic Schema format
func ConvertParametersToSchema(params tools.FunctionParameters) anthropic.ToolInputSchemaParam {
	properties := params.Properties
	if properties == nil {
		properties = map[string]any{}
	}

	return anthropic.ToolInputSchemaParam{
		Properties: properties,
		Required:   params.Required,
	}
}

func (c *Client) ID() string {
	return c.config.Provider + "/" + c.config.Model
}
