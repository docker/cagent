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
	fileManager      *FileManager
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
				httpclient.WithModelName(cfg.Name),
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

	// Initialize FileManager for file uploads
	anthropicClient.fileManager = NewFileManager(anthropicClient.clientFn)

	return anthropicClient, nil
}

// hasFileAttachments checks if any messages contain file attachments.
// This is used to determine if we need to use the Beta API (Files API is Beta-only).
func hasFileAttachments(messages []chat.Message) bool {
	for i := range messages {
		for _, part := range messages[i].MultiContent {
			if part.Type == chat.MessagePartTypeFile && part.File != nil {
				return true
			}
		}
	}
	return false
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
	// 2. Structured output is configured, or
	// 3. Messages contain file attachments (Files API is Beta-only)
	// Note: Structured outputs require beta header support (only available on BetaMessageNewParams)
	if c.interleavedThinkingEnabled() || c.ModelOptions.StructuredOutput() != nil || hasFileAttachments(messages) {
		return c.createBetaStream(ctx, client, messages, requestTools, maxTokens)
	}

	allTools, err := convertTools(requestTools)
	if err != nil {
		slog.Error("Failed to convert tools for Anthropic request", "error", err)
		return nil, err
	}

	converted, err := c.convertMessages(ctx, messages)
	if err != nil {
		slog.Error("Failed to convert messages for Anthropic request", "error", err)
		return nil, err
	}
	// convertMessages enforces Anthropic tool sequencing strictly; if it's invalid,
	// convertMessages returns an error.
	if len(converted) == 0 {
		return nil, errors.New("no messages to send after conversion: all messages were filtered out")
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

func (c *Client) convertMessages(ctx context.Context, messages []chat.Message) ([]anthropic.MessageParam, error) {
	var anthropicMessages []anthropic.MessageParam
	// Track the set of tool_use IDs from the last appended assistant message.
	// Anthropic requires the immediate next message to be a user message containing
	// tool_result blocks for those IDs (and only those IDs).
	var pendingToolUseIDs map[string]struct{}

	for i := 0; i < len(messages); i++ {
		msg := &messages[i]
		if msg.Role == chat.MessageRoleSystem {
			// System messages are handled via the top-level params.System
			continue
		}
		if msg.Role == chat.MessageRoleUser {
			if pendingToolUseIDs != nil {
				return nil, errors.New("assistant tool_use must be immediately followed by tool results")
			}
			// Handle MultiContent for user messages (including images and files)
			if len(msg.MultiContent) > 0 {
				contentBlocks, err := c.convertUserMultiContent(ctx, msg.MultiContent)
				if err != nil {
					return nil, err
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
			if pendingToolUseIDs != nil {
				return nil, errors.New("assistant tool_use must be immediately followed by tool results")
			}
			contentBlocks := make([]anthropic.ContentBlockParamUnion, 0)

			// Include thinking blocks when present to preserve extended thinking context
			if msg.ReasoningContent != "" && msg.ThinkingSignature != "" {
				contentBlocks = append(contentBlocks, anthropic.NewThinkingBlock(msg.ThinkingSignature, msg.ReasoningContent))
			} else if msg.ThinkingSignature != "" {
				contentBlocks = append(contentBlocks, anthropic.NewRedactedThinkingBlock(msg.ThinkingSignature))
			}

			if len(msg.ToolCalls) > 0 {
				pendingToolUseIDs = make(map[string]struct{}, len(msg.ToolCalls))
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
					if toolCall.ID != "" {
						pendingToolUseIDs[toolCall.ID] = struct{}{}
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
				pendingToolUseIDs = nil
			}
			continue
		}
		if msg.Role == chat.MessageRoleTool {
			if pendingToolUseIDs == nil {
				return nil, fmt.Errorf("unexpected tool result without preceding tool_use (tool_use_id=%q)", messages[i].ToolCallID)
			}
			// Group consecutive tool results into a single user message.
			//
			// This is to satisfy Anthropic's requirement that tool_use blocks are immediately followed
			// by a single user message containing all corresponding tool_result blocks.
			var blocks []anthropic.ContentBlockParamUnion
			hadToolMessages := false
			j := i
			for j < len(messages) && messages[j].Role == chat.MessageRoleTool {
				hadToolMessages = true
				id := messages[j].ToolCallID
				if strings.TrimSpace(id) == "" {
					return nil, errors.New("tool result is missing tool_use_id")
				}
				if _, ok := pendingToolUseIDs[id]; !ok {
					return nil, fmt.Errorf("unexpected tool_result tool_use_id=%q", id)
				}
				tr := anthropic.NewToolResultBlock(id, strings.TrimSpace(messages[j].Content), messages[j].IsError)
				blocks = append(blocks, tr)
				delete(pendingToolUseIDs, id)
				j++
			}
			if hadToolMessages && len(blocks) == 0 {
				return nil, errors.New("tool_result messages present but none match pending tool_use ids")
			}
			if len(pendingToolUseIDs) > 0 {
				missing := make([]string, 0, len(pendingToolUseIDs))
				for id := range pendingToolUseIDs {
					missing = append(missing, id)
				}
				return nil, fmt.Errorf("missing tool_result for tool_use id %s (and %d more)", missing[0], len(missing)-1)
			}
			if len(blocks) > 0 {
				anthropicMessages = append(anthropicMessages, anthropic.NewUserMessage(blocks...))
			}
			pendingToolUseIDs = nil
			i = j - 1
			continue
		}
	}

	if pendingToolUseIDs != nil {
		return nil, errors.New("assistant tool_use present but no subsequent tool results")
	}

	// Add ephemeral cache to last 2 messages' last content block
	applyMessageCacheControl(anthropicMessages)

	return anthropicMessages, nil
}

// convertUserMultiContent converts user message multi-content parts to Anthropic content blocks.
// It handles text and images (base64 and URL). File uploads are NOT supported in the non-Beta API
// and will return an error - callers should use hasFileAttachments() to route to the Beta API.
func (c *Client) convertUserMultiContent(_ context.Context, parts []chat.MessagePart) ([]anthropic.ContentBlockParamUnion, error) {
	contentBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(parts))

	for _, part := range parts {
		switch part.Type {
		case chat.MessagePartTypeText:
			if txt := strings.TrimSpace(part.Text); txt != "" {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(txt))
			}

		case chat.MessagePartTypeImageURL:
			if part.ImageURL == nil {
				continue
			}
			// Handle base64 data URLs (legacy format)
			if strings.HasPrefix(part.ImageURL.URL, "data:") {
				urlParts := strings.SplitN(part.ImageURL.URL, ",", 2)
				if len(urlParts) == 2 {
					mediaTypePart := urlParts[0]
					base64Data := urlParts[1]

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
						mediaType = "image/jpeg"
					}

					contentBlocks = append(contentBlocks, anthropic.NewImageBlock(anthropic.Base64ImageSourceParam{
						Data:      base64Data,
						MediaType: anthropic.Base64ImageSourceMediaType(mediaType),
					}))
				}
			} else if strings.HasPrefix(part.ImageURL.URL, "http://") || strings.HasPrefix(part.ImageURL.URL, "https://") {
				// URL-based images
				contentBlocks = append(contentBlocks, anthropic.NewImageBlock(anthropic.URLImageSourceParam{
					URL: part.ImageURL.URL,
				}))
			}

		case chat.MessagePartTypeFile:
			if part.File == nil {
				continue
			}

			// File uploads require the Beta API - this code path should not be reached
			// if hasFileAttachments() correctly routes to createBetaStream().
			// Return a clear error if we somehow get here.
			return nil, fmt.Errorf("file attachments require the Beta API; use hasFileAttachments() to route correctly (path=%q, file_id=%q)",
				part.File.Path, part.File.FileID)
		}
	}

	return contentBlocks, nil
}

// createFileContentBlock creates the appropriate content block for a file based on its MIME type.
// Note: File uploads via the Files API require the Beta API. This function supports images
// (which have OfFile in the Beta API only) and documents. For non-Beta API usage with files,
// the caller should handle the conversion differently or use base64 encoding.
func createFileContentBlock(fileID, mimeType string) (anthropic.ContentBlockParamUnion, error) {
	// The standard (non-Beta) API doesn't support file references in ImageBlockParamSourceUnion
	// or DocumentBlockParamSourceUnion. Files API is Beta-only.
	// For now, we return an error directing users to use the Beta API path.
	return anthropic.ContentBlockParamUnion{}, fmt.Errorf("file uploads require the Beta API; file_id=%s, mime_type=%s", fileID, mimeType)
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

// CleanupFiles removes all files uploaded during this session from Anthropic's storage.
func (c *Client) CleanupFiles(ctx context.Context) error {
	if c.fileManager == nil {
		return nil
	}
	return c.fileManager.CleanupAll(ctx)
}

// FileManager returns the file manager for this client, allowing external cleanup.
// Returns nil if file uploads are not supported or not initialized.
func (c *Client) FileManager() *FileManager {
	return c.fileManager
}

// Tool sequencing validation helpers removed; conversion enforces sequencing strictly.

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
