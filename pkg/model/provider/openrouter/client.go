package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	openai "github.com/sashabaranov/go-openai"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client implements the provider for OpenRouter using the OpenAI-compatible SDK.
// OpenRouter speaks the OpenAI Chat Completions API with a different base URL
// and requires the OPENROUTER_API_KEY header.
// Docs: https://openrouter.ai/docs#chat-completions

type Client struct {
	client *openai.Client
	config *latest.ModelConfig
}

func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("model configuration is required")
	}
	if cfg.Provider != "openrouter" {
		return nil, errors.New("model type must be 'openrouter'")
	}

	apiKey := env.Get(ctx, "OPENROUTER_API_KEY")
	if apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY environment variable is required")
	}

	// OpenRouter is OpenAI-compatible at base URL https://openrouter.ai/api/v1
	cfgURL := cfg.BaseURL
	if strings.TrimSpace(cfgURL) == "" {
		cfgURL = "https://openrouter.ai/api/v1"
	}

	oc := openai.DefaultConfig(apiKey)
	oc.BaseURL = cfgURL
	// OpenRouter requires HTTP header: HTTP-Referer (optional) and X-Title (optional).
	// The go-openai client allows custom headers via a custom http.Client Transport.
	// We'll attach X-Title if provided via cfg.ProviderOpts["x_title"].
	if cfg.ProviderOpts != nil {
		if v, ok := cfg.ProviderOpts["x_title"].(string); ok && v != "" {
			oc.HTTPClient = withHeader(oc.HTTPClient, map[string]string{"X-Title": v})
		}
		if v, ok := cfg.ProviderOpts["http_referer"].(string); ok && v != "" {
			oc.HTTPClient = withHeader(oc.HTTPClient, map[string]string{"HTTP-Referer": v})
		}
	}

	client := openai.NewClientWithConfig(oc)
	return &Client{client: client, config: cfg}, nil
}

func withHeader(base openai.HTTPDoer, add map[string]string) openai.HTTPDoer {
	if base == nil {
		base = http.DefaultClient
	}
	return headerDoer{base: base, add: add}
}

type headerDoer struct {
	base openai.HTTPDoer
	add  map[string]string
}

func (h headerDoer) Do(req *http.Request) (*http.Response, error) {
	for k, v := range h.add {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	return h.base.Do(req)
}

func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating OpenRouter chat completion stream",
		"model", c.config.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	if len(messages) == 0 {
		return nil, errors.New("at least one message is required")
	}

	req := openai.ChatCompletionRequest{
		Model:            c.config.Model,
		Messages:         convertMessages(messages),
		Temperature:      float32(c.config.Temperature),
		TopP:             float32(c.config.TopP),
		FrequencyPenalty: float32(c.config.FrequencyPenalty),
		PresencePenalty:  float32(c.config.PresencePenalty),
		Stream:           true,
		StreamOptions:    &openai.StreamOptions{IncludeUsage: true},
	}
	if c.config.MaxTokens > 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	if len(requestTools) > 0 {
		req.Tools = make([]openai.Tool, len(requestTools))
		for i, tool := range requestTools {
			req.Tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Strict:      tool.Function.Strict,
					Parameters:  tool.Function.Parameters,
				},
			}
			if len(tool.Function.Parameters.Properties) == 0 {
				req.Tools[i].Function.Parameters = json.RawMessage("{}")
			}
		}
		if c.config.ParallelToolCalls != nil {
			req.ParallelToolCalls = *c.config.ParallelToolCalls
		}
	}

	stream, err := c.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}
	return newStreamAdapter(stream), nil
}

func (c *Client) CreateChatCompletion(
	ctx context.Context,
	messages []chat.Message,
) (string, error) {
	req := openai.ChatCompletionRequest{
		Model:    c.config.Model,
		Messages: convertMessages(messages),
	}
	if c.config.MaxTokens > 0 {
		req.MaxTokens = c.config.MaxTokens
	}
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Choices[0].Message.Content, nil
}

func (c *Client) ID() string { return c.config.Provider + "/" + c.config.Model }

func convertMessages(messages []chat.Message) []openai.ChatCompletionMessage {
	openaiMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i := range messages {
		msg := &messages[i]
		m := openai.ChatCompletionMessage{Role: string(msg.Role), Name: msg.Name}
		if len(msg.MultiContent) == 0 {
			m.Content = msg.Content
		} else {
			m.MultiContent = convertMultiContent(msg.MultiContent)
		}
		if msg.FunctionCall != nil {
			m.FunctionCall = &openai.FunctionCall{Name: msg.FunctionCall.Name, Arguments: msg.FunctionCall.Arguments}
		}
		if len(msg.ToolCalls) > 0 {
			m.ToolCalls = make([]openai.ToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				m.ToolCalls[j] = openai.ToolCall{ID: tc.ID, Type: openai.ToolType(tc.Type), Function: openai.FunctionCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments}}
			}
		}
		if msg.ToolCallID != "" {
			m.ToolCallID = msg.ToolCallID
		}
		openaiMessages[i] = m
	}
	return openaiMessages
}

func convertMultiContent(multiContent []chat.MessagePart) []openai.ChatMessagePart {
	out := make([]openai.ChatMessagePart, len(multiContent))
	for i, part := range multiContent {
		p := openai.ChatMessagePart{Type: openai.ChatMessagePartType(part.Type), Text: part.Text}
		if part.Type == chat.MessagePartTypeImageURL && part.ImageURL != nil {
			p.ImageURL = &openai.ChatMessageImageURL{URL: part.ImageURL.URL, Detail: openai.ImageURLDetail(part.ImageURL.Detail)}
		}
		out[i] = p
	}
	return out
}
