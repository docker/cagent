package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
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
	clientFn func(context.Context) (anthropic.Client, error)
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

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	var clientFn func(context.Context) (anthropic.Client, error)
	if gateway := globalOptions.Gateway(); gateway == "" {
		authToken := env.Get(ctx, "ANTHROPIC_API_KEY")
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
		clientFn = func(context.Context) (anthropic.Client, error) {
			return client, nil
		}
	} else {
		// Fail fast if Docker Desktop's auth token isn't available
		if env.Get(ctx, environment.DockerDesktopTokenEnv) == "" {
			slog.Error("Anthropic client creation failed", "error", "failed to get Docker Desktop's authentication token")
			return nil, errors.New("sorry, you first need to sign in Docker Desktop to use the Docker AI Gateway")
		}

		// When using a Gateway, tokens are short-lived.
		clientFn = func(ctx context.Context) (anthropic.Client, error) {
			// Query a fresh auth token each time the client is used
			authToken := env.Get(ctx, environment.DockerDesktopTokenEnv)
			if authToken == "" {
				return anthropic.Client{}, errors.New("failed to get Docker Desktop token for Gateway")
			}

			return anthropic.NewClient(
				option.WithAuthToken(authToken),
				option.WithAPIKey(authToken),
				option.WithBaseURL(gateway),
				option.WithHTTPClient(httpclient.NewHTTPClient(
					httpclient.WithProxiedBaseURL(defaultsTo(cfg.BaseURL, "https://api.anthropic.com/")),
					httpclient.WithProvider(cfg.Provider),
					httpclient.WithModel(cfg.Model),
				)),
			), nil
		}
	}

	slog.Debug("Anthropic client created successfully", "model", cfg.Model)

	if globalOptions.StructuredOutput() != nil {
		return nil, errors.New("anthropic does not support native structured_output")
	}

	return &Client{
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
			Env:          env,
		},
		clientFn: clientFn,
	}, nil
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

	maxTokens := int64(c.ModelConfig.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 8192 // Default output budget when not specified
	}

	client, err := c.clientFn(ctx)
	if err != nil {
		slog.Error("Failed to create Anthropic client", "error", err)
		return nil, err
	}

	// Use Beta API with interleaved thinking only when enabled
	if c.interleavedThinkingEnabled() {
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
	// Preflight-cap max_tokens using Anthropic's count-tokens API
	sys := extractSystemBlocks(messages)
	if used, err := countAnthropicTokens(ctx, client, anthropic.Model(c.ModelConfig.Model), converted, sys, allTools); err == nil {
		configuredMaxTokens := maxTokens
		maxTokens = clampMaxTokens(anthropicContextLimit(c.ModelConfig.Model), used, maxTokens)
		if maxTokens < configuredMaxTokens {
			slog.Warn("Anthropic API max_tokens clamped to", "max_tokens", maxTokens)
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.ModelConfig.Model),
		MaxTokens: maxTokens,
		Messages:  converted,
		Tools:     allTools,
	}

	// Populate proper Anthropic system prompt from input messages
	if len(sys) > 0 {
		params.System = sys
	}

	// Apply thinking budget
	if c.ModelConfig.ThinkingBudget != nil && c.ModelConfig.ThinkingBudget.Tokens > 0 {
		thinkingTokens := int64(c.ModelConfig.ThinkingBudget.Tokens)
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
	ad := newStreamAdapter(stream)
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

// Helpers for map-based inspection
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
	if remaining < 1 {
		remaining = 1
	}
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

func defaultsTo(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}
