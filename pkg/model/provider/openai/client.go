package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents an OpenAI client wrapper
// It implements the provider.Provider interface
type Client struct {
	base.Config
	clientFn func(context.Context) (*openai.Client, error)
}

// NewClient creates a new OpenAI client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("OpenAI client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	var clientFn func(context.Context) (*openai.Client, error)
	if gateway := globalOptions.Gateway(); gateway == "" {
		key := cfg.TokenKey
		if key == "" {
			key = "OPENAI_API_KEY"
		}
		authToken := env.Get(ctx, key)
		if authToken == "" {
			return nil, fmt.Errorf("%s environment variable is required", key)
		}

		var openaiConfig openai.ClientConfig
		if cfg.Provider == "azure" {
			openaiConfig = openai.DefaultAzureConfig(authToken, cfg.BaseURL)
			openaiConfig.AzureModelMapperFunc = func(model string) string {
				// NOTE(krissetto): This is to preserve dots in deployment names.
				// Only strip colons like the library already does to minimize code drift.
				// Can be removed once fixed/changed upstream. See https://github.com/sashabaranov/go-openai/issues/978

				// only 3.5 models have the "." stripped in their names
				if strings.Contains(model, "3.5") {
					return regexp.MustCompile(`[.:]`).ReplaceAllString(model, "")
				}
				return strings.ReplaceAll(model, ":", "")
			}
		} else {
			openaiConfig = openai.DefaultConfig(authToken)
		}

		if cfg.BaseURL != "" {
			openaiConfig.BaseURL = cfg.BaseURL
		}

		openaiConfig.HTTPClient = httpclient.NewHTTPClient()

		// TODO: Move this logic to ProviderAliases as a config function
		if cfg.ProviderOpts != nil {
			switch cfg.Provider { //nolint:gocritic
			case "azure":
				if apiVersion, exists := cfg.ProviderOpts["api_version"]; exists {
					slog.Debug("Setting API version", "api_version", apiVersion)
					if apiVersionStr, ok := apiVersion.(string); ok {
						openaiConfig.APIVersion = apiVersionStr
					}
				}
			}
		}

		slog.Debug("OpenAI API key found, creating client")
		client := openai.NewClientWithConfig(openaiConfig)
		clientFn = func(context.Context) (*openai.Client, error) {
			return client, nil
		}
	} else {
		// Fail fast if Docker Desktop's auth token isn't available
		if env.Get(ctx, environment.DockerDesktopTokenEnv) == "" {
			slog.Error("OpenAI client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		// When using a Gateway, tokens are short-lived.
		clientFn = func(ctx context.Context) (*openai.Client, error) {
			// Query a fresh auth token each time the client is used
			authToken := env.Get(ctx, environment.DockerDesktopTokenEnv)
			if authToken == "" {
				return nil, errors.New("failed to get Docker Desktop token for Gateway")
			}

			openaiConfig := openai.DefaultConfig(authToken)
			openaiConfig.BaseURL = gateway + "/v1"
			openaiConfig.HTTPClient = httpclient.NewHTTPClient(
				httpclient.WithProxiedBaseURL(defaultsTo(cfg.BaseURL, "https://api.openai.com/v1")),
				httpclient.WithProvider(cfg.Provider),
				httpclient.WithModel(cfg.Model),
			)

			return openai.NewClientWithConfig(openaiConfig), nil
		}
	}

	slog.Debug("OpenAI client created successfully", "model", cfg.Model)

	return &Client{
		Config: base.Config{
			ModelConfig:  cfg,
			ModelOptions: globalOptions,
		},
		clientFn: clientFn,
	}, nil
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
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for i := range messages {
		msg := &messages[i]

		// Skip invalid assistant messages upfront. This can happen if the model is out of tokens (max_tokens reached)
		if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) == 0 && len(msg.MultiContent) == 0 && strings.TrimSpace(msg.Content) == "" {
			continue
		}

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

		openaiMessages = append(openaiMessages, openaiMessage)
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
		"model", c.ModelConfig.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	if len(messages) == 0 {
		slog.Error("OpenAI stream creation failed", "error", "at least one message is required")
		return nil, errors.New("at least one message is required")
	}

	trackUsage := c.ModelConfig.TrackUsage == nil || *c.ModelConfig.TrackUsage

	request := openai.ChatCompletionRequest{
		Model:            c.ModelConfig.Model,
		Messages:         convertMessages(messages),
		Temperature:      float32(c.ModelConfig.Temperature),
		TopP:             float32(c.ModelConfig.TopP),
		FrequencyPenalty: float32(c.ModelConfig.FrequencyPenalty),
		PresencePenalty:  float32(c.ModelConfig.PresencePenalty),
		Stream:           true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: trackUsage,
		},
	}

	if c.MaxTokens() > 0 {
		if !isResponsesOnlyModel(c.ModelConfig.Model) {
			request.MaxTokens = c.MaxTokens()
			slog.Debug("OpenAI request configured with max tokens", "max_tokens", c.MaxTokens())
		} else {
			request.MaxCompletionTokens = c.MaxTokens()
			slog.Debug("using max_completion_tokens instead of max_tokens for Responses-API models", "model", c.ModelConfig.Model)
		}
	}

	if len(requestTools) > 0 {
		slog.Debug("Adding tools to OpenAI request", "tool_count", len(requestTools))
		request.Tools = make([]openai.Tool, len(requestTools))
		for i, tool := range requestTools {
			parameters, err := ConvertParametersToSchema(tool.Parameters)
			if err != nil {
				slog.Debug("Failed to convert tool parameters to OpenAI schema", "tool_name", tool.Name, "error", err)
				return nil, err
			}

			request.Tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  parameters,
				},
			}

			slog.Debug("Added tool to OpenAI request", "tool_name", tool.Name)
		}
		if c.ModelConfig.ParallelToolCalls != nil {
			request.ParallelToolCalls = *c.ModelConfig.ParallelToolCalls
		}
	}

	// Apply thinking budget: set reasoning_effort parameter
	if c.ModelConfig.ThinkingBudget != nil {
		effort, err := getOpenAIReasoningEffort(c.ModelConfig)
		if err != nil {
			slog.Error("OpenAI request using thinking_budget failed", "error", err)
			return nil, err
		}
		request.ReasoningEffort = effort
		slog.Debug("OpenAI request using thinking_budget", "reasoning_effort", effort)
	}

	// Apply structured output configuration
	if structuredOutput := c.ModelOptions.StructuredOutput(); structuredOutput != nil {
		slog.Debug("OpenAI request using structured output", "name", structuredOutput.Name, "strict", structuredOutput.Strict)

		request.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:        structuredOutput.Name,
				Description: structuredOutput.Description,
				Schema:      jsonSchema(structuredOutput.Schema),
				Strict:      structuredOutput.Strict,
			},
		}
	}

	// Log the request in JSON format for debugging
	if requestJSON, err := json.Marshal(request); err == nil {
		slog.Debug("OpenAI chat completion request", "request", string(requestJSON))
	} else {
		slog.Error("Failed to marshal OpenAI request to JSON", "error", err)
	}

	client, err := c.clientFn(ctx)
	if err != nil {
		slog.Error("Failed to create OpenAI client", "error", err)
		return nil, err
	}

	stream, err := client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		// Fallback for future models: retry without max_tokens if server complains
		if isMaxTokensUnsupportedError(err) {
			slog.Debug("Retrying OpenAI stream without max_tokens due to server requirement", "model", c.ModelConfig.Model)
			request.MaxTokens = 0
			stream, err = client.CreateChatCompletionStream(ctx, request)
		}
		if err != nil {
			slog.Error("OpenAI stream creation failed", "error", err, "model", c.ModelConfig.Model)
			return nil, err
		}
	}

	slog.Debug("OpenAI chat completion stream created successfully", "model", c.ModelConfig.Model)
	return newStreamAdapter(stream, trackUsage), nil
}

// ConvertParametersToSchema converts parameters to OpenAI Schema format
func ConvertParametersToSchema(params any) (any, error) {
	return tools.SchemaToMap(params)
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

func isOpenAIReasoningModel(model string) bool {
	m := strings.ToLower(model)
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

// getOpenAIReasoningEffort resolves the reasoning effort value from the
// model configuration's ThinkingBudget. Returns the effort (minimal|low|medium|high) or an error
func getOpenAIReasoningEffort(cfg *latest.ModelConfig) (effort string, err error) {
	if cfg == nil || cfg.ThinkingBudget == nil {
		return "", nil
	}

	if !isOpenAIReasoningModel(cfg.Model) {
		slog.Warn("OpenAI reasoning effort is not supported for this model, ignoring thinking_budget", "model", cfg.Model)
		return "", nil
	}

	effort = strings.TrimSpace(strings.ToLower(cfg.ThinkingBudget.Effort))
	if effort == "minimal" || effort == "low" || effort == "medium" || effort == "high" {
		return effort, nil
	}

	return "", fmt.Errorf("OpenAI requests only support 'minimal', 'low', 'medium', 'high' as values for thinking_budget effort, got effort: '%s', tokens: '%d'", effort, cfg.ThinkingBudget.Tokens)
}

// jsonSchema is a helper type that implements json.Marshaler for map[string]any
// This allows us to pass schema maps to the OpenAI library which expects json.Marshaler
type jsonSchema map[string]any

func (j jsonSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any(j))
}

func defaultsTo(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}
