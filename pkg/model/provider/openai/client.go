package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/desktop"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents an OpenAI client wrapper
// It implements the provider.Provider interface
type Client struct {
	client *openai.Client
	config *latest.ModelConfig
	// When using the Docker AI Gateway, tokens are short-lived. We rebuild
	// the client per request using these fields.
	useGateway     bool
	gatewayBaseURL string
}

// NewClient creates a new OpenAI client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("OpenAI client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "openai" {
		slog.Error("OpenAI client creation failed", "error", "model type must be 'openai'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'openai'")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	isAzureOpenAI := false
	if cfg.ProviderOpts != nil {
		for k := range cfg.ProviderOpts {
			if strings.HasPrefix(k, "azure_") {
				isAzureOpenAI = true
				slog.Debug("Azure OpenAI detected", "provider_opt", k)
				break
			}
		}
	}

	if isAzureOpenAI && strings.TrimSpace(cfg.BaseURL) == "" {
		errMsg := "base_url is required for Azure OpenAI"
		slog.Error("OpenAI client creation failed", "error", "base_url is required for Azure OpenAI", "model", cfg.Model)
		return nil, errors.New(errMsg)
	}

	var openaiConfig openai.ClientConfig
	if gateway := globalOptions.Gateway(); gateway == "" {
		key := cfg.TokenKey
		if key == "" {
			if isAzureOpenAI {
				key = "AZURE_OPENAI_API_KEY"
			} else {
				key = "OPENAI_API_KEY"
			}
		}
		authToken := env.Get(ctx, key)
		if authToken == "" {
			if key == cfg.TokenKey {
				errMsg := fmt.Sprintf("%s key configured in model config is required", key)
				return nil, errors.New(errMsg)
			}
			if isAzureOpenAI {
				return nil, errors.New("AZURE_OPENAI_API_KEY environment variable is required")
			}
			return nil, errors.New("OPENAI_API_KEY environment variable is required")
		}

		// Configure Azure vs standard OpenAI
		if cfg.BaseURL != "" && isAzureOpenAI {
			azureBase := strings.ToLower(strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"))
			openaiConfig = openai.DefaultAzureConfig(authToken, azureBase)
			// api-version is required by Azure; enforce presence via provider_opts.azure_api_version
			if cfg.ProviderOpts == nil {
				return nil, errors.New("provider_opts.azure_api_version is required for Azure OpenAI")
			}
			if v, ok := cfg.ProviderOpts["azure_api_version"]; !ok {
				return nil, errors.New("provider_opts.azure_api_version is required for Azure OpenAI")
			} else {
				if s, ok := v.(string); !ok || s == "" {
					return nil, errors.New("provider_opts.azure_api_version is required for Azure OpenAI")
				} else {
					openaiConfig.APIVersion = s
				}
			}
			// allow overriding deployment name (Azure uses deployment name in URL path)
			if v, ok := cfg.ProviderOpts["azure_deployment_name"]; ok {
				if s, ok := v.(string); ok && s != "" {
					name := s
					openaiConfig.AzureModelMapperFunc = func(string) string { return name }
				}
			}
		} else {
			openaiConfig = openai.DefaultConfig(authToken)
			if cfg.BaseURL != "" {
				openaiConfig.BaseURL = cfg.BaseURL
			}
		}
	} else {
		authToken := desktop.GetToken(ctx)
		if authToken == "" {
			slog.Error("OpenAI client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		openaiConfig = openai.DefaultConfig(authToken)
		openaiConfig.BaseURL = gateway + "/v1"
		// mark gateway usage for per-request token refresh
		// we persist the base URL to rebuild clients on demand
		// even though we also create an initial client here
		// so the first request works immediately
	}

	useGateway := false
	gatewayBaseURL := ""
	if globalOptions.Gateway() != "" {
		useGateway = true
		gatewayBaseURL = globalOptions.Gateway() + "/v1"
	}

	slog.Debug("OpenAI API key found, creating client")
	client := openai.NewClientWithConfig(openaiConfig)
	slog.Debug("OpenAI client created successfully", "model", cfg.Model)

	return &Client{
		client:         client,
		config:         cfg,
		useGateway:     useGateway,
		gatewayBaseURL: gatewayBaseURL,
	}, nil
}

// newGatewayClient builds a new OpenAI client using a fresh Docker Desktop token.
func (c *Client) newGatewayClient(ctx context.Context) *openai.Client {
	authToken := desktop.GetToken(ctx)
	cfg := openai.DefaultConfig(authToken)
	cfg.BaseURL = c.gatewayBaseURL
	return openai.NewClientWithConfig(cfg)
}

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

// convertMessages converts chat.ChatCompletionMessage to openai.ChatCompletionMessage
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

// CreateChatCompletionStream creates a streaming chat completion request
// It returns a stream that can be iterated over to get completion chunks
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating OpenAI chat completion stream",
		"model", c.config.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	if len(messages) == 0 {
		slog.Error("OpenAI stream creation failed", "error", "at least one message is required")
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
		if !isResponsesOnlyModel(c.config.Model) {
			request.MaxTokens = c.config.MaxTokens
			slog.Debug("OpenAI request configured with max tokens", "max_tokens", c.config.MaxTokens)
		} else {
			request.MaxCompletionTokens = c.config.MaxTokens
			slog.Debug("using max_completion_tokens instead of max_tokens for Responses-API models", "model", c.config.Model)
		}
	}

	if len(requestTools) > 0 {
		slog.Debug("Adding tools to OpenAI request", "tool_count", len(requestTools))
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
			slog.Debug("Added tool to OpenAI request", "tool_name", tool.Function.Name)
		}
		if c.config.ParallelToolCalls != nil {
			request.ParallelToolCalls = *c.config.ParallelToolCalls
		}
	}

	// Log the request in JSON format for debugging
	if requestJSON, err := json.Marshal(request); err == nil {
		slog.Debug("OpenAI chat completion request", "request", string(requestJSON))
	} else {
		slog.Error("Failed to marshal OpenAI request to JSON", "error", err)
	}

	// Build a fresh client per request when using the gateway
	client := c.client
	if c.useGateway {
		client = c.newGatewayClient(ctx)
	}
	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		// Fallback for future models: retry without max_tokens if server complains
		if isMaxTokensUnsupportedError(err) {
			slog.Debug("Retrying OpenAI stream without max_tokens due to server requirement", "model", c.config.Model)
			request.MaxTokens = 0
			stream, err = client.CreateChatCompletionStream(ctx, request)
		}
		if err != nil {
			slog.Error("OpenAI stream creation failed", "error", err, "model", c.config.Model)
			return nil, err
		}
	}

	slog.Debug("OpenAI chat completion stream created successfully", "model", c.config.Model)
	return newStreamAdapter(stream), nil
}

func (c *Client) CreateChatCompletion(
	ctx context.Context,
	messages []chat.Message,
) (string, error) {
	slog.Debug("Creating OpenAI chat completion", "model", c.config.Model, "message_count", len(messages))

	request := openai.ChatCompletionRequest{
		Model:    c.config.Model,
		Messages: convertMessages(messages),
	}

	// Set appropriate token limit depending on model family
	if c.config.MaxTokens > 0 {
		if isResponsesOnlyModel(c.config.Model) {
			request.MaxCompletionTokens = c.config.MaxTokens
		} else {
			request.MaxTokens = c.config.MaxTokens
		}
	}

	// Build a fresh client per request when using the gateway
	client := c.client
	if c.useGateway {
		client = c.newGatewayClient(ctx)
	}
	response, err := client.CreateChatCompletion(ctx, request)
	if err != nil {
		// Fallback for future models: retry without max_tokens if server complains
		if isMaxTokensUnsupportedError(err) {
			slog.Debug("Retrying OpenAI request without max_tokens due to server requirement", "model", c.config.Model)
			request.MaxTokens = 0
			request.MaxCompletionTokens = c.config.MaxTokens
			response, err = client.CreateChatCompletion(ctx, request)
		}
		if err != nil {
			slog.Error("OpenAI chat completion failed", "error", err, "model", c.config.Model)
			return "", err
		}
	}

	slog.Debug("OpenAI chat completion successful", "model", c.config.Model, "response_length", len(response.Choices[0].Message.Content))
	return response.Choices[0].Message.Content, nil
}

// isResponsesOnlyModel returns true for newer OpenAI models that use the Responses API
// and expect max_completion_tokens/max_output_tokens instead of max_tokens
func isResponsesOnlyModel(model string) bool {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gpt-4.1") {
		return true
	}
	if strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4") {
		return true
	}
	if strings.HasPrefix(m, "gpt-5") {
		return true
	}
	return false
}

// isMaxTokensUnsupportedError returns true if the error indicates the server expects
// max_completion_tokens instead of max_tokens (Responses API models)
func isMaxTokensUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	e := strings.ToLower(err.Error())
	return strings.Contains(e, "this model is not supported maxtokens") ||
		strings.Contains(e, "use maxcompletiontokens")
}

func (c *Client) ID() string {
	return c.config.Provider + "/" + c.config.Model
}
