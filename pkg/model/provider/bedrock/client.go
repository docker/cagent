// Package bedrock implements the AWS Bedrock provider for cagent.
// It supports multiple model families including Anthropic Claude, Amazon Titan,
// Meta Llama, and Mistral models via AWS Bedrock Runtime API.
// Authentication can be done via AWS credentials or bearer token.
package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// bearerTokenTransport wraps an http.RoundTripper and adds Bearer token authentication
type bearerTokenTransport struct {
	token     string
	transport http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request to avoid modifying the original
	clonedReq := req.Clone(req.Context())
	clonedReq.Header.Set("Authorization", "Bearer "+t.token)
	return t.transport.RoundTrip(clonedReq)
}

// Client represents a Bedrock client wrapper implementing provider.Provider
type Client struct {
	client       *bedrockruntime.Client
	config       *latest.ModelConfig
	modelOptions options.ModelOptions
	region       string
}

// NewClient creates a new Bedrock client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("Bedrock client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "amazon-bedrock" {
		slog.Error("Bedrock client creation failed", "error", "model type must be 'amazon-bedrock'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'amazon-bedrock'")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	// Determine region from config or environment
	region := "us-east-1" // default
	if cfg.ProviderOpts != nil {
		if r, ok := cfg.ProviderOpts["region"]; ok {
			if regionStr, ok := r.(string); ok && regionStr != "" {
				region = regionStr
			}
		}
	}
	if envRegion := env.Get(ctx, "AWS_REGION"); envRegion != "" {
		region = envRegion
	}

	// Check for bearer token authentication first
	bearerToken := env.Get(ctx, "AWS_BEDROCK_TOKEN")

	var awsCfg aws.Config
	var err error

	if bearerToken != "" {
		slog.Debug("Bedrock using bearer token authentication", "token_present", true)
		// For bearer token auth (proxy/gateway scenarios), we provide static credentials
		// to satisfy the SDK's auth requirements, but our custom HTTP transport will
		// replace the Authorization header with the bearer token.
		staticCreds := credentials.NewStaticCredentialsProvider("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "")

		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(region),
			config.WithCredentialsProvider(staticCreds),
			config.WithHTTPClient(&http.Client{
				Transport: &bearerTokenTransport{
					token:     bearerToken,
					transport: http.DefaultTransport,
				},
			}),
		)
		if err != nil {
			slog.Error("Failed to load AWS config for bearer token auth", "error", err)
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
	} else {
		// Use standard AWS credential chain
		slog.Debug("Bedrock using AWS credential chain", "region", region)
		awsCfg, err = config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			slog.Error("Failed to load AWS config", "error", err)
			return nil, fmt.Errorf("failed to load AWS config: %w", err)
		}
	}

	// Build client options
	clientOpts := []func(*bedrockruntime.Options){
		func(o *bedrockruntime.Options) {
			o.Region = region
		},
	}

	// Add custom endpoint if specified
	if cfg.BaseURL != "" {
		slog.Debug("Bedrock using custom endpoint", "endpoint", cfg.BaseURL)
		clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
			o.BaseEndpoint = aws.String(cfg.BaseURL)
		})
	}

	client := bedrockruntime.NewFromConfig(awsCfg, clientOpts...)
	slog.Debug("Bedrock client created successfully", "model", cfg.Model, "region", region)

	return &Client{
		client:       client,
		config:       cfg,
		modelOptions: globalOptions,
		region:       region,
	}, nil
}

// CreateChatCompletionStream creates a streaming chat completion request
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating Bedrock chat completion stream",
		"model", c.config.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools),
		"region", c.region)

	if len(messages) == 0 {
		slog.Error("Bedrock stream creation failed", "error", "at least one message is required")
		return nil, errors.New("at least one message is required")
	}

	// Determine model family from model ID
	modelFamily := getModelFamily(c.config.Model)
	slog.Debug("Detected model family", "model", c.config.Model, "family", modelFamily)

	// Build request body based on model family
	var requestBody []byte
	var err error

	switch modelFamily {
	case "anthropic":
		requestBody, err = c.buildClaudeRequest(messages, requestTools)
	case "titan":
		requestBody, err = c.buildTitanRequest(messages)
	case "llama", "mistral":
		requestBody, err = c.buildLlamaMistralRequest(messages)
	default:
		return nil, fmt.Errorf("unsupported model family: %s", modelFamily)
	}

	if err != nil {
		slog.Error("Failed to build request body", "error", err, "model_family", modelFamily)
		return nil, fmt.Errorf("failed to build request body: %w", err)
	}

	slog.Debug("Bedrock request body built", "model", c.config.Model, "body_size", len(requestBody))

	// Invoke model with streaming
	input := &bedrockruntime.InvokeModelWithResponseStreamInput{
		ModelId:     aws.String(c.config.Model),
		Body:        requestBody,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	}

	output, err := c.client.InvokeModelWithResponseStream(ctx, input)
	if err != nil {
		slog.Error("Bedrock stream creation failed", "error", err, "model", c.config.Model)
		return nil, fmt.Errorf("failed to invoke model: %w", err)
	}

	slog.Debug("Bedrock chat completion stream created successfully", "model", c.config.Model)
	return newStreamAdapter(output.GetStream().Events(), c.config.Model, modelFamily), nil
}

// buildClaudeRequest builds request for Anthropic Claude models
func (c *Client) buildClaudeRequest(messages []chat.Message, requestTools []tools.Tool) ([]byte, error) {
	// Claude via Bedrock uses Messages API format similar to Anthropic provider
	claudeMessages := []map[string]any{}
	var systemPrompt string

	for _, msg := range messages {
		if msg.Role == chat.MessageRoleSystem {
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += msg.Content
			continue
		}

		claudeMsg := map[string]any{
			"role": string(msg.Role),
		}

		// Handle tool responses
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID != "" {
			claudeMsg["role"] = "user"
			claudeMsg["content"] = []map[string]any{
				{
					"type":        "tool_result",
					"tool_use_id": msg.ToolCallID,
					"content":     msg.Content,
				},
			}
			claudeMessages = append(claudeMessages, claudeMsg)
			continue
		}

		// Handle assistant messages with tool calls
		if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) > 0 {
			content := []map[string]any{}
			if msg.Content != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": msg.Content,
				})
			}
			for _, tc := range msg.ToolCalls {
				// Initialize input as empty map to ensure it's never nil
				// Bedrock requires input to be a valid dictionary, not null
				input := make(map[string]any)
				if tc.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						// If unmarshal fails, log it but continue with empty map
						slog.Warn("Failed to unmarshal tool arguments", "tool", tc.Function.Name, "error", err)
					}
				}
				content = append(content, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Function.Name,
					"input": input,
				})
			}
			claudeMsg["content"] = content
			claudeMessages = append(claudeMessages, claudeMsg)
			continue
		}

		// Regular messages
		claudeMsg["content"] = msg.Content
		claudeMessages = append(claudeMessages, claudeMsg)
	}

	request := map[string]any{
		"anthropic_version": "bedrock-2023-05-31",
		"messages":          claudeMessages,
	}

	if systemPrompt != "" {
		request["system"] = systemPrompt
	}

	// Add model parameters
	if c.config.MaxTokens > 0 {
		request["max_tokens"] = c.config.MaxTokens
	} else {
		request["max_tokens"] = 4096 // Default for Claude
	}

	if c.config.Temperature > 0 {
		request["temperature"] = c.config.Temperature
	}

	if c.config.TopP > 0 {
		request["top_p"] = c.config.TopP
	}

	// Add tools if provided
	if len(requestTools) > 0 {
		claudeTools := []map[string]any{}
		for _, tool := range requestTools {
			inputSchema, err := tools.SchemaToMap(tool.Parameters)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool parameters: %w", err)
			}
			claudeTools = append(claudeTools, map[string]any{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": inputSchema,
			})
		}
		request["tools"] = claudeTools
	}

	return json.Marshal(request)
}

// buildTitanRequest builds request for Amazon Titan models
func (c *Client) buildTitanRequest(messages []chat.Message) ([]byte, error) {
	// Titan uses a simpler format - concatenate all messages
	var prompt strings.Builder
	for _, msg := range messages {
		if msg.Content != "" {
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n")
		}
	}

	request := map[string]any{
		"inputText": strings.TrimSpace(prompt.String()),
		"textGenerationConfig": map[string]any{
			"maxTokenCount": c.config.MaxTokens,
			"temperature":   c.config.Temperature,
			"topP":          c.config.TopP,
		},
	}

	return json.Marshal(request)
}

// buildLlamaMistralRequest builds request for Meta Llama and Mistral models
func (c *Client) buildLlamaMistralRequest(messages []chat.Message) ([]byte, error) {
	// Llama/Mistral use instruction format
	var prompt strings.Builder
	for _, msg := range messages {
		if msg.Role == chat.MessageRoleSystem {
			prompt.WriteString("<<SYS>>\n")
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n<</SYS>>\n\n")
		} else if msg.Role == chat.MessageRoleUser {
			prompt.WriteString("[INST] ")
			prompt.WriteString(msg.Content)
			prompt.WriteString(" [/INST]\n")
		} else if msg.Role == chat.MessageRoleAssistant {
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n")
		}
	}

	request := map[string]any{
		"prompt": strings.TrimSpace(prompt.String()),
	}

	if c.config.MaxTokens > 0 {
		request["max_gen_len"] = c.config.MaxTokens
	}

	if c.config.Temperature > 0 {
		request["temperature"] = c.config.Temperature
	}

	if c.config.TopP > 0 {
		request["top_p"] = c.config.TopP
	}

	return json.Marshal(request)
}

// getModelFamily determines the model family from the model ID
func getModelFamily(modelID string) string {
	lower := strings.ToLower(modelID)
	switch {
	case strings.Contains(lower, "anthropic.claude"):
		return "anthropic"
	case strings.Contains(lower, "amazon.titan"):
		return "titan"
	case strings.Contains(lower, "meta.llama"):
		return "llama"
	case strings.Contains(lower, "mistral"):
		return "mistral"
	default:
		return "unknown"
	}
}

// ID returns the model provider ID
func (c *Client) ID() string {
	return c.config.Provider + "/" + c.config.Model
}

// Options returns the effective model options used by this client
func (c *Client) Options() options.ModelOptions {
	return c.modelOptions
}
