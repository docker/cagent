package requesty

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v1"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents a Requesty client
type Client struct {
	client  *openai.Client
	config  *latest.ModelConfig
}

// headerTransport wraps http.RoundTripper to add custom headers
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Add custom headers to the request
	for key, value := range t.headers {
		req.Header.Set(key, value)
	}
	return t.base.RoundTrip(req)
}

// NewClient creates a new Requesty client
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("Requesty client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "requesty" {
		slog.Error("Requesty client creation failed", "error", "model type must be 'requesty'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'requesty'")
	}

	// Set default base_url for Requesty if not provided
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://router.requesty.ai/v1"
		slog.Debug("Using default Requesty base_url", "base_url", baseURL)
	}

	// Set default headers for Requesty
	headers := make(map[string]string)
	if cfg.Headers != nil {
		for k, v := range cfg.Headers {
			headers[k] = v
		}
	}
	if _, ok := headers["HTTP-Referer"]; !ok {
		headers["HTTP-Referer"] = "https://github.com/docker/cagent"
	}
	if _, ok := headers["X-Title"]; !ok {
		headers["X-Title"] = "Cagent"
	}

	// Get auth token
	key := cfg.TokenKey
	if key == "" {
		key = "REQUESTY_API_KEY"
	}
	authToken, err := env.Get(ctx, key)
	if err != nil || authToken == "" {
		slog.Error("Requesty client creation failed", "error", "failed to get authentication token", "details", err)
		return nil, errors.New("REQUESTY_API_KEY environment variable is required")
	}

	// Create OpenAI config with Requesty settings
	openaiConfig := openai.DefaultConfig(authToken)
	openaiConfig.BaseURL = baseURL

	// Create HTTP client with custom headers
	httpClient := &http.Client{
		Transport: &headerTransport{
			headers: headers,
			base:    http.DefaultTransport,
		},
	}
	openaiConfig.HTTPClient = httpClient

	// Create OpenAI client with Requesty configuration
	client := openai.NewClientWithConfig(openaiConfig)

	slog.Debug("Requesty client created successfully", "model", cfg.Model, "base_url", baseURL, "headers", headers)

	return &Client{
		client: client,
		config: cfg,
	}, nil
}

// ID returns the provider ID
func (c *Client) ID() string {
	return "requesty"
}

// CreateChatCompletionStream creates a streaming chat completion
func (c *Client) CreateChatCompletionStream(ctx context.Context, messages []chat.Message, requestTools []tools.Tool) (chat.MessageStream, error) {
	slog.Debug("Creating Requesty chat completion stream", "model", c.config.Model, "message_count", len(messages), "tool_count", len(requestTools))

	if len(messages) == 0 {
		slog.Error("Requesty stream creation failed", "error", "at least one message is required")
		return nil, errors.New("at least one message is required")
	}

	request := openai.ChatCompletionRequest{
		Model:            c.config.Model,
		Messages:         convertMessages(messages),
		Temperature:      float32(c.config.Temperature),
		TopP:             float32(c.config.TopP),
		FrequencyPenalty: float32(c.config.FrequencyPenalty),
		PresencePenalty:  float32(c.config.PresencePenalty),
		Stream:           true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
	}

	if c.config.MaxTokens > 0 {
		request.MaxTokens = c.config.MaxTokens
	}

	if c.config.ParallelToolCalls != nil {
		request.ParallelToolCalls = *c.config.ParallelToolCalls
	}

	// Add tools if provided
	if len(requestTools) > 0 {
		slog.Debug("Adding tools to Requesty request", "tool_count", len(requestTools))
		request.Tools = make([]openai.Tool, len(requestTools))
		for i, tool := range requestTools {
			request.Tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Strict:      tool.Function.Strict,
					Parameters:  tool.Function.Parameters,
				},
			}
			if len(tool.Function.Parameters.Properties) == 0 {
				request.Tools[i].Function.Parameters = json.RawMessage("{}")
			}
			slog.Debug("Added tool to Requesty request", "tool_name", tool.Function.Name)
		}
	}

	stream, err := c.client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		slog.Error("Requesty stream creation failed", "error", err, "model", c.config.Model)
		return nil, err
	}

	slog.Debug("Requesty chat completion stream created successfully", "model", c.config.Model)
	return newStreamAdapter(stream), nil
}

// CreateChatCompletion creates a non-streaming chat completion
func (c *Client) CreateChatCompletion(ctx context.Context, messages []chat.Message) (string, error) {
	slog.Debug("Creating Requesty chat completion", "model", c.config.Model, "message_count", len(messages))

	request := openai.ChatCompletionRequest{
		Model:    c.config.Model,
		Messages: convertMessages(messages),
	}

	if c.config.MaxTokens > 0 {
		request.MaxTokens = c.config.MaxTokens
	}

	if c.config.ParallelToolCalls != nil {
		request.ParallelToolCalls = *c.config.ParallelToolCalls
	}

	response, err := c.client.CreateChatCompletion(ctx, request)
	if err != nil {
		slog.Error("Requesty chat completion failed", "error", err, "model", c.config.Model)
		return "", err
	}

	slog.Debug("Requesty chat completion successful", "model", c.config.Model, "response_length", len(response.Choices[0].Message.Content))
	return response.Choices[0].Message.Content, nil
}

// Helper functions (copied from OpenAI provider)
func convertMultiContent(multiContent []chat.MessagePart) []openai.ChatMessagePart {
	openaiMultiContent := make([]openai.ChatMessagePart, len(multiContent))
	for i, part := range multiContent {
		openaiPart := openai.ChatMessagePart{
			Type: openai.ChatMessagePartType(part.Type),
			Text: part.Text,
		}

		// Handle image URL conversion
		if part.Type == chat.MessagePartTypeImageURL && part.ImageURL != nil {
			openaiPart.ImageURL = &openai.ChatMessageImageURL{
				URL:    part.ImageURL.URL,
				Detail: openai.ImageURLDetail(part.ImageURL.Detail),
			}
		}

		openaiMultiContent[i] = openaiPart
	}
	return openaiMultiContent
}

func convertMessages(messages []chat.Message) []openai.ChatCompletionMessage {
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i := range messages {
		msg := &messages[i]
		openaiMessage := openai.ChatCompletionMessage{
			Role: string(msg.Role),
			Name: msg.Name,
		}

		if len(msg.MultiContent) == 0 {
			openaiMessage.Content = msg.Content
		} else {
			openaiMessage.MultiContent = convertMultiContent(msg.MultiContent)
		}

		if msg.FunctionCall != nil {
			openaiMessage.FunctionCall = &openai.FunctionCall{
				Name:      msg.FunctionCall.Name,
				Arguments: msg.FunctionCall.Arguments,
			}
		}

		if len(msg.ToolCalls) > 0 {
			openaiMessage.ToolCalls = make([]openai.ToolCall, len(msg.ToolCalls))
			for j, toolCall := range msg.ToolCalls {
				openaiMessage.ToolCalls[j] = openai.ToolCall{
					ID:   toolCall.ID,
					Type: openai.ToolType(toolCall.Type),
					Function: openai.FunctionCall{
						Name:      toolCall.Function.Name,
						Arguments: toolCall.Function.Arguments,
					},
				}
			}
		}

		if msg.ToolCallID != "" {
			openaiMessage.ToolCallID = msg.ToolCallID
		}

		openaiMessages[i] = openaiMessage
	}
	return openaiMessages
}
