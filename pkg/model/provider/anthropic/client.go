package anthropic

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/httpclient"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents an Anthropic client wrapper implementing provider.Provider
// It holds the anthropic client and model config
type Client struct {
	base.Config
	clientFn         func(context.Context) (anthropic.Client, error)
	lastHTTPResponse *http.Response
}

func (c *Client) getResponseTrailer() http.Header {
	if c.lastHTTPResponse == nil {
		return nil
	}

	if c.lastHTTPResponse.Body != nil {
		_, _ = io.Copy(io.Discard, c.lastHTTPResponse.Body)
	}

	return c.lastHTTPResponse.Trailer
}

// adjustMaxTokensForThinking checks if max_tokens needs adjustment for thinking_budget.
// Anthropic's max_tokens represents the combined budget for thinking + output tokens.
// Returns the adjusted maxTokens value and an error if user-set max_tokens is too low.
func (c *Client) adjustMaxTokensForThinking(maxTokens int64) (int64, error) {
	if c.ModelConfig.ThinkingBudget == nil || c.ModelConfig.ThinkingBudget.Tokens <= 0 {
		return maxTokens, nil
	}

	thinkingTokens := int64(c.ModelConfig.ThinkingBudget.Tokens)
	minRequired := thinkingTokens + 1024 // configured thinking budget + minimum output buffer

	if maxTokens <= thinkingTokens {
		userSetMaxTokens := c.ModelConfig.MaxTokens != nil
		if userSetMaxTokens {
			// User explicitly set max_tokens too low - return error
			slog.Error("Anthropic: max_tokens must be greater than thinking_budget",
				"max_tokens", maxTokens,
				"thinking_budget", thinkingTokens)
			return 0, fmt.Errorf("anthropic: max_tokens (%d) must be greater than thinking_budget (%d); increase max_tokens to at least %d",
				maxTokens, thinkingTokens, minRequired)
		}
		// Auto-adjust when user didn't set max_tokens
		slog.Info("Anthropic: auto-adjusting max_tokens to accommodate thinking_budget",
			"original_max_tokens", maxTokens,
			"thinking_budget", thinkingTokens,
			"new_max_tokens", minRequired)
		// return the configured thinking budget + 8192 because that's the default
		// max_tokens value for anthropic models when unspecified by the user
		return thinkingTokens + 8192, nil
	}

	return maxTokens, nil
}

// interleavedThinkingEnabled returns false unless explicitly enabled via
// models:provider_opts:interleaved_thinking: true
func (c *Client) interleavedThinkingEnabled() bool {
	// Default to false if not provided
	if c == nil || len(c.ModelConfig.ProviderOpts) == 0 {
		return false
	}
	v, ok := c.ModelConfig.ProviderOpts["interleaved_thinking"]
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

	if env == nil {
		slog.Error("Anthropic client creation failed", "error", "environment provider is required")
		return nil, errors.New("environment provider is required")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&globalOptions)
		}
	}

	anthropicClient := &Client{
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
			Env:          env,
		},
	}

	if gateway := globalOptions.Gateway(); gateway == "" {
		authToken, _ := env.Get(ctx, "ANTHROPIC_API_KEY")
		if authToken == "" {
			return nil, errors.New("ANTHROPIC_API_KEY environment variable is required")
		}

		slog.Debug("Anthropic API key found, creating client")
		requestOptions := []option.RequestOption{
			option.WithAPIKey(authToken),
			option.WithHTTPClient(httpclient.NewHTTPClient()),
		}
		if cfg.BaseURL != "" {
			requestOptions = append(requestOptions, option.WithBaseURL(cfg.BaseURL))
		}
		client := anthropic.NewClient(requestOptions...)
		anthropicClient.clientFn = func(context.Context) (anthropic.Client, error) {
			return client, nil
		}
	} else {
		// Fail fast if Docker Desktop's auth token isn't available
		if token, _ := env.Get(ctx, environment.DockerDesktopTokenEnv); token == "" {
			slog.Error("Anthropic client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		// When using a Gateway, tokens are short-lived.
		anthropicClient.clientFn = func(ctx context.Context) (anthropic.Client, error) {
			// Query a fresh auth token each time the client is used
			authToken, _ := env.Get(ctx, environment.DockerDesktopTokenEnv)
			if authToken == "" {
				return anthropic.Client{}, errors.New("failed to get Docker Desktop token for Gateway")
			}

			url, err := url.Parse(gateway)
			if err != nil {
				return anthropic.Client{}, fmt.Errorf("invalid gateway URL: %w", err)
			}
			baseURL := fmt.Sprintf("%s://%s%s/", url.Scheme, url.Host, url.Path)

			// Configure a custom HTTP client to inject headers and query params used by the Gateway.
			httpOptions := []httpclient.Opt{
				httpclient.WithProxiedBaseURL(cmp.Or(cfg.BaseURL, "https://api.anthropic.com/")),
				httpclient.WithProvider(cfg.Provider),
				httpclient.WithModel(cfg.Model),
				httpclient.WithQuery(url.Query()),
			}
			if globalOptions.GeneratingTitle() {
				httpOptions = append(httpOptions, httpclient.WithHeader("X-Cagent-GeneratingTitle", "1"))
			}

			client := anthropic.NewClient(
				option.WithResponseInto(&anthropicClient.lastHTTPResponse),
				option.WithAuthToken(authToken),
				option.WithAPIKey(authToken),
				option.WithBaseURL(baseURL),
				option.WithHTTPClient(httpclient.NewHTTPClient(httpOptions...)),
			)

			return client, nil
		}
	}

	slog.Debug("Anthropic client created successfully", "model", cfg.Model)

	return anthropicClient, nil
}

// CreateChatCompletionStream creates a streaming chat completion request
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating Anthropic chat completion stream",
		"model", c.ModelConfig.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	// Default to 8192 if maxTokens is not set (0)
	// This is a safe default that works for all Anthropic models
	maxTokens := c.ModelOptions.MaxTokens()
	if maxTokens == 0 {
		maxTokens = 8192
	}
	maxTokens, err := c.adjustMaxTokensForThinking(maxTokens)
	if err != nil {
		return nil, err
	}

	client, err := c.clientFn(ctx)
	if err != nil {
		slog.Error("Failed to create Anthropic client", "error", err)
		return nil, err
	}

	// Use Beta API when:
	// 1. Interleaved thinking is enabled, or
	// 2. Structured output is configured
	// Note: Structured outputs require beta header support (only available on BetaMessageNewParams)
	if c.interleavedThinkingEnabled() || c.ModelOptions.StructuredOutput() != nil {
		return c.createBetaStream(ctx, client, messages, requestTools, maxTokens)
	}

	allTools, err := convertTools(requestTools)
	if err != nil {
		slog.Error("Failed to convert tools for Anthropic request", "error", err)
		return nil, err
	}

	converted := convertMessages(messages)
	// Preflight validation to ensure tool_use/tool_result sequencing is valid
	if err := validateAnthropicSequencing(converted); err != nil {
		slog.Warn("Invalid message sequencing for Anthropic detected, attempting self-repair", "error", err)
		converted = repairAnthropicSequencing(converted)
		if err2 := validateAnthropicSequencing(converted); err2 != nil {
			slog.Error("Failed to self-repair Anthropic sequencing", "error", err2)
			return nil, err
		}
	}
	sys := extractSystemBlocks(messages)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.ModelConfig.Model),
		MaxTokens: maxTokens,
		System:    sys,
		Messages:  converted,
		Tools:     allTools,
	}

	// Apply thinking budget first, as it affects whether we can set temperature
	thinkingEnabled := false
	if c.ModelConfig.ThinkingBudget != nil && c.ModelConfig.ThinkingBudget.Tokens > 0 {
		thinkingTokens := int64(c.ModelConfig.ThinkingBudget.Tokens)
		switch {
		case thinkingTokens >= 1024 && thinkingTokens < maxTokens:
			params.Thinking = anthropic.ThinkingConfigParamOfEnabled(thinkingTokens)
			thinkingEnabled = true
			slog.Debug("Anthropic API using thinking_budget (standard messages)", "budget_tokens", thinkingTokens)
		case thinkingTokens >= maxTokens:
			slog.Warn("Anthropic thinking_budget must be less than max_tokens, ignoring", "tokens", thinkingTokens, "max_tokens", maxTokens)
		default:
			slog.Warn("Anthropic thinking_budget below minimum (1024), ignoring", "tokens", thinkingTokens)
		}
	}

	// Temperature and TopP cannot be set when extended thinking is enabled
	// (Anthropic requires temperature=1.0 which is the default when thinking is on)
	if !thinkingEnabled {
		if c.ModelConfig.Temperature != nil {
			params.Temperature = param.NewOpt(*c.ModelConfig.Temperature)
		}
		if c.ModelConfig.TopP != nil {
			params.TopP = param.NewOpt(*c.ModelConfig.TopP)
		}
	} else if c.ModelConfig.Temperature != nil || c.ModelConfig.TopP != nil {
		slog.Debug("Anthropic extended thinking enabled, ignoring temperature/top_p settings")
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

	// Add fine-grained tool streaming beta header
	betaHeader := option.WithHeader("anthropic-beta", "fine-grained-tool-streaming-2025-05-14")

	stream := client.Messages.NewStreaming(ctx, params, betaHeader)
	trackUsage := c.ModelConfig.TrackUsage == nil || *c.ModelConfig.TrackUsage
	ad := c.newStreamAdapter(stream, trackUsage)

	// Set up single retry for context length errors
	ad.retryFn = func() *streamAdapter {
		used, err := countAnthropicTokens(ctx, client, anthropic.Model(c.ModelConfig.Model), converted, sys, allTools)
		if err != nil {
			slog.Warn("Failed to count tokens for retry, skipping", "error", err)
			return nil
		}
		newMaxTokens := clampMaxTokens(anthropicContextLimit(c.ModelConfig.Model), used, maxTokens)
		if newMaxTokens >= maxTokens {
			slog.Warn("Token count does not require clamping, not retrying")
			return nil
		}
		slog.Warn("Retrying with clamped max_tokens after context length error", "original max_tokens", maxTokens, "clamped max_tokens", newMaxTokens, "used tokens", used)
		retryParams := params
		retryParams.MaxTokens = newMaxTokens
		return c.newStreamAdapter(client.Messages.NewStreaming(ctx, retryParams, betaHeader), trackUsage)
	}

	slog.Debug("Anthropic chat completion stream created successfully", "model", c.ModelConfig.Model)
	return ad, nil
}

func convertMessages(messages []chat.Message) []anthropic.MessageParam {
	var anthropicMessages []anthropic.MessageParam
	// Track whether the last appended assistant message included tool_use blocks
	// so we can ensure the immediate next message is the grouped tool_result user message.
	pendingAssistantToolUse := false

	for i := 0; i < len(messages); i++ {
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

								// Use SDK helper with proper typed source for better performance
								// (avoids JSON marshal/unmarshal round trip)
								contentBlocks = append(contentBlocks, anthropic.NewImageBlock(anthropic.Base64ImageSourceParam{
									Data:      base64Data,
									MediaType: anthropic.Base64ImageSourceMediaType(mediaType),
								}))
							}
						} else if strings.HasPrefix(part.ImageURL.URL, "http://") || strings.HasPrefix(part.ImageURL.URL, "https://") {
							// Support URL-based images - Anthropic can fetch images directly from URLs
							contentBlocks = append(contentBlocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{
								URL: part.ImageURL.URL,
							}))
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
				// Mark that we expect the very next message to be the grouped tool_result blocks.
				pendingAssistantToolUse = true
			} else {
				if txt := strings.TrimSpace(msg.Content); txt != "" {
					contentBlocks = append(contentBlocks, anthropic.NewTextBlock(txt))
				}
				if len(contentBlocks) > 0 {
					anthropicMessages = append(anthropicMessages, anthropic.NewAssistantMessage(contentBlocks...))
				}
				// No tool_use in this assistant message
				pendingAssistantToolUse = false
			}
			continue
		}
		if msg.Role == chat.MessageRoleTool {
			// Group consecutive tool results into a single user message.
			//
			// This is to satisfy Anthropic's requirement that tool_use blocks are immediately followed
			// by a single user message containing all corresponding tool_result blocks.
			var blocks []anthropic.ContentBlockParamUnion
			j := i
			for j < len(messages) && messages[j].Role == chat.MessageRoleTool {
				tr := anthropic.NewToolResultBlock(messages[j].ToolCallID, strings.TrimSpace(messages[j].Content), false)
				blocks = append(blocks, tr)
				j++
			}
			if len(blocks) > 0 {
				// Only include tool_result blocks if they immediately follow an assistant
				// message that contained tool_use. Otherwise, drop them to avoid invalid
				// sequencing errors.
				if pendingAssistantToolUse {
					anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(blocks...))
				}
				// Whether we used them or not, we've now handled the expected tool_result slot.
				pendingAssistantToolUse = false
			}
			i = j - 1
			continue
		}
	}

	// Add ephemeral cache to last 2 messages' last content block
	applyMessageCacheControl(anthropicMessages)

	return anthropicMessages
}

// applyMessageCacheControl adds ephemeral cache control to the last content block
// of the last 2 messages for prompt caching.
func applyMessageCacheControl(messages []anthropic.MessageParam) {
	for i := len(messages) - 1; i >= 0 && i >= len(messages)-2; i-- {
		msg := &messages[i]
		if len(msg.Content) == 0 {
			continue
		}
		lastIdx := len(msg.Content) - 1
		block := &msg.Content[lastIdx]
		cacheCtrl := anthropic.NewCacheControlEphemeralParam()
		switch {
		case block.OfText != nil:
			block.OfText.CacheControl = cacheCtrl
		case block.OfToolUse != nil:
			block.OfToolUse.CacheControl = cacheCtrl
		case block.OfToolResult != nil:
			block.OfToolResult.CacheControl = cacheCtrl
		case block.OfImage != nil:
			block.OfImage.CacheControl = cacheCtrl
		case block.OfDocument != nil:
			block.OfDocument.CacheControl = cacheCtrl
		}
	}
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
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: txt,
			})
		}

		if msg.CacheControl {
			systemBlocks[len(systemBlocks)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
		}
	}

	return systemBlocks
}

func convertTools(tooles []tools.Tool) ([]anthropic.ToolUnionParam, error) {
	toolParams := make([]anthropic.ToolParam, len(tooles))

	for i, tool := range tooles {
		inputSchema, err := ConvertParametersToSchema(tool.Parameters)
		if err != nil {
			return nil, err
		}

		toolParams[i] = anthropic.ToolParam{
			Name:        tool.Name,
			Description: anthropic.String(tool.Description),
			InputSchema: inputSchema,
		}
	}
	anthropicTools := make([]anthropic.ToolUnionParam, len(toolParams))
	for i := range toolParams {
		anthropicTools[i] = anthropic.ToolUnionParam{OfTool: &toolParams[i]}
	}

	return anthropicTools, nil
}

// ConvertParametersToSchema converts parameters to Anthropic Schema format
func ConvertParametersToSchema(params any) (anthropic.ToolInputSchemaParam, error) {
	var schema anthropic.ToolInputSchemaParam
	if err := tools.ConvertSchema(params, &schema); err != nil {
		return anthropic.ToolInputSchemaParam{}, err
	}

	return schema, nil
}

func (c *Client) ID() string {
	return c.ModelConfig.Provider + "/" + c.ModelConfig.Model
}

// validateAnthropicSequencing verifies that for every assistant message that includes
// one or more tool_use blocks, the immediately following message is a user message
// that includes tool_result blocks for all those tool_use IDs (grouped into that single message).
func validateAnthropicSequencing(msgs []anthropic.MessageParam) error {
	// Marshal-based inspection to avoid depending on SDK internals of union types
	for i := range msgs {
		m, ok := marshalToMap(msgs[i])
		if !ok || m["role"] != "assistant" {
			continue
		}

		toolUseIDs := collectToolUseIDs(contentArray(m))
		if len(toolUseIDs) == 0 {
			continue
		}

		if i+1 >= len(msgs) {
			slog.Warn("Anthropic sequencing invalid: assistant tool_use present but no next user tool_result message", "assistant_index", i)
			return errors.New("assistant tool_use present but no subsequent user message with tool_result blocks")
		}

		next, ok := marshalToMap(msgs[i+1])
		if !ok || next["role"] != "user" {
			slog.Warn("Anthropic sequencing invalid: next message after assistant tool_use is not user", "assistant_index", i, "next_role", next["role"])
			return errors.New("assistant tool_use must be followed by a user message containing corresponding tool_result blocks")
		}

		toolResultIDs := collectToolResultIDs(contentArray(next))
		missing := differenceIDs(toolUseIDs, toolResultIDs)
		if len(missing) > 0 {
			slog.Warn("Anthropic sequencing invalid: missing tool_result for tool_use id in next user message", "assistant_index", i, "tool_use_id", missing[0], "missing_count", len(missing))
			return fmt.Errorf("missing tool_result for tool_use id %s in the next user message", missing[0])
		}
	}
	return nil
}

// repairAnthropicSequencing inserts a synthetic user message containing tool_result blocks
// immediately after any assistant message that has tool_use blocks missing a corresponding
// tool_result in the next user message. This is a best-effort local repair to keep the
// conversation valid for Anthropic while preserving original messages, to keep the agent loop running.
func repairAnthropicSequencing(msgs []anthropic.MessageParam) []anthropic.MessageParam {
	if len(msgs) == 0 {
		return msgs
	}
	repaired := make([]anthropic.MessageParam, 0, len(msgs)+2)
	for i := range msgs {
		repaired = append(repaired, msgs[i])

		m, ok := marshalToMap(msgs[i])
		if !ok || m["role"] != "assistant" {
			continue
		}

		toolUseIDs := collectToolUseIDs(contentArray(m))
		if len(toolUseIDs) == 0 {
			continue
		}

		// Remove any IDs that already have results in the next user message
		if i+1 < len(msgs) {
			if next, ok := marshalToMap(msgs[i+1]); ok && next["role"] == "user" {
				toolResultIDs := collectToolResultIDs(contentArray(next))
				for id := range toolResultIDs {
					delete(toolUseIDs, id)
				}
			}
		}

		if len(toolUseIDs) > 0 {
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(toolUseIDs))
			for id := range toolUseIDs {
				blocks = append(blocks, anthropic.NewToolResultBlock(id, "(tool execution failed)", false))
			}
			repaired = append(repaired, anthropic.NewUserMessage(blocks...))
		}
	}
	return repaired
}

// marshalToMap is a helper that converts any value to a map[string]any via JSON marshaling.
// This is used to inspect SDK union types without depending on their internal structure.
// It's shared by both standard and Beta API validation/repair code.
func marshalToMap(v any) (map[string]any, bool) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil, false
	}
	return m, true
}

// contentArray extracts the content array from a marshaled message map.
// Used by both standard and Beta API validation/repair code.
func contentArray(m map[string]any) []any {
	if a, ok := m["content"].([]any); ok {
		return a
	}
	return nil
}

func collectToolUseIDs(content []any) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, c := range content {
		if cb, ok := c.(map[string]any); ok {
			if t, _ := cb["type"].(string); t == "tool_use" {
				if id, _ := cb["id"].(string); id != "" {
					ids[id] = struct{}{}
				}
			}
		}
	}
	return ids
}

func collectToolResultIDs(content []any) map[string]struct{} {
	ids := make(map[string]struct{})
	for _, c := range content {
		if cb, ok := c.(map[string]any); ok {
			if t, _ := cb["type"].(string); t == "tool_result" {
				if id, _ := cb["tool_use_id"].(string); id != "" {
					ids[id] = struct{}{}
				}
			}
		}
	}
	return ids
}

func differenceIDs(a, b map[string]struct{}) []string {
	if len(a) == 0 {
		return nil
	}
	var missing []string
	for id := range a {
		if _, ok := b[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}

// anthropicContextLimit returns a reasonable default context window for Anthropic models.
// We default to 200k tokens, which is what 3.5-4.5 models support; adjust as needed over time.
func anthropicContextLimit(model string) int64 {
	_ = model
	return 200000
}

// clampMaxTokens returns the effective max_tokens value after capping to the
// remaining context window (limit - used - safety), clamped to at least 1.
func clampMaxTokens(limit, used, configured int64) int64 {
	const safety = int64(1024)

	remaining := limit - used - safety
	remaining = max(remaining, 1)
	if configured > remaining {
		return remaining
	}
	return configured
}

// countAnthropicTokens calls Anthropic's Count Tokens API for the provided payload
// and returns the number of input tokens.
func countAnthropicTokens(
	ctx context.Context,
	client anthropic.Client,
	model anthropic.Model,
	messages []anthropic.MessageParam,
	system []anthropic.TextBlockParam,
	anthropicTools []anthropic.ToolUnionParam,
) (int64, error) {
	params := anthropic.MessageCountTokensParams{
		Model:    model,
		Messages: messages,
	}
	if len(system) > 0 {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{
			OfTextBlockArray: system,
		}
	}
	if len(anthropicTools) > 0 {
		// Convert ToolUnionParam to MessageCountTokensToolUnionParam
		toolParams := make([]anthropic.MessageCountTokensToolUnionParam, len(anthropicTools))
		for i, tool := range anthropicTools {
			if tool.OfTool != nil {
				toolParams[i] = anthropic.MessageCountTokensToolUnionParam{
					OfTool: tool.OfTool,
				}
			}
		}
		params.Tools = toolParams
	}

	result, err := client.Messages.CountTokens(ctx, params)
	if err != nil {
		return 0, err
	}
	return result.InputTokens, nil
}
