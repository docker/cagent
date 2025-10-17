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

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	cagentTools "github.com/docker/cagent/pkg/tools"
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
	base.Config
	client *bedrockruntime.Client
	region string
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

	// Determine region: explicit config takes precedence over environment variable
	region := "us-east-1" // default
	if envRegion := env.Get(ctx, "AWS_REGION"); envRegion != "" {
		region = envRegion
	}
	// Explicit ProviderOpts config takes precedence over environment variable
	if cfg.ProviderOpts != nil {
		if r, ok := cfg.ProviderOpts["region"]; ok {
			if regionStr, ok := r.(string); ok && regionStr != "" {
				region = regionStr
			}
		}
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
		// The following credentials are AWS documentation example credentials (see:
		// https://docs.aws.amazon.com/general/latest/gr/aws-sec-cred-types.html#access-keys-and-secret-access-keys).
		// They are intentionally fake and used only to satisfy the SDK's authentication requirements
		// when using bearer token authentication. The actual authorization is handled by the
		// bearerTokenTransport, which replaces the Authorization header with the bearer token.
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
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
			Env:          env,
		},
		client: client,
		region: region,
	}, nil
}

// CreateChatCompletionStream creates a streaming chat completion request using Converse API
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []cagentTools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating Bedrock chat completion stream",
		"model", c.ModelConfig.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools),
		"region", c.region)

	if len(messages) == 0 {
		slog.Error("Bedrock stream creation failed", "error", "at least one message is required")
		return nil, errors.New("at least one message is required")
	}

	// Convert messages to Converse API format
	converseMessages, systemBlocks, err := convertToConverseMessages(messages)
	if err != nil {
		slog.Error("Failed to convert messages", "error", err)
		return nil, fmt.Errorf("failed to convert messages: %w", err)
	}

	// Build Converse API input
	input := &bedrockruntime.ConverseStreamInput{
		ModelId:  aws.String(c.ModelConfig.Model),
		Messages: converseMessages,
	}

	// Add system prompts if present
	if len(systemBlocks) > 0 {
		input.System = systemBlocks
	}

	// Add inference configuration
	inferenceConfig := &types.InferenceConfiguration{}
	if c.ModelConfig.MaxTokens > 0 {
		inferenceConfig.MaxTokens = aws.Int32(int32(c.ModelConfig.MaxTokens))
	}
	if c.ModelConfig.Temperature != nil {
		inferenceConfig.Temperature = aws.Float32(float32(*c.ModelConfig.Temperature))
	}
	if c.ModelConfig.TopP != nil {
		inferenceConfig.TopP = aws.Float32(float32(*c.ModelConfig.TopP))
	}
	input.InferenceConfig = inferenceConfig

	// Add tools if provided
	if len(requestTools) > 0 {
		converseTools, err := convertToConverseTools(requestTools)
		if err != nil {
			slog.Error("Failed to convert tools", "error", err)
			return nil, fmt.Errorf("failed to convert tools: %w", err)
		}
		input.ToolConfig = &types.ToolConfiguration{
			Tools: converseTools,
		}
	}

	// Invoke model with streaming
	output, err := c.client.ConverseStream(ctx, input)
	if err != nil {
		slog.Error("Bedrock stream creation failed", "error", err, "model", c.ModelConfig.Model)
		return nil, fmt.Errorf("failed to invoke model: %w", err)
	}

	slog.Debug("Bedrock chat completion stream created successfully", "model", c.ModelConfig.Model)
	return newStreamAdapter(output, c.ModelConfig.Model), nil
}

// convertToConverseMessages converts cagent messages to Converse API format
func convertToConverseMessages(messages []chat.Message) ([]types.Message, []types.SystemContentBlock, error) {
	var converseMessages []types.Message
	var systemBlocks []types.SystemContentBlock

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		// System messages are handled separately
		if msg.Role == chat.MessageRoleSystem {
			systemBlocks = append(systemBlocks, &types.SystemContentBlockMemberText{
				Value: msg.Content,
			})
			continue
		}

		// Convert role
		var role types.ConversationRole
		switch msg.Role {
		case chat.MessageRoleUser:
			role = types.ConversationRoleUser
		case chat.MessageRoleAssistant:
			role = types.ConversationRoleAssistant
		case chat.MessageRoleTool:
			// Tool results are sent as user messages with tool result blocks
			role = types.ConversationRoleUser
		default:
			return nil, nil, fmt.Errorf("unsupported message role: %s", msg.Role)
		}

		// Build content blocks
		var contentBlocks []types.ContentBlock

		// Handle tool results - group consecutive tool results into one user message
		if msg.Role == chat.MessageRoleTool && msg.ToolCallID != "" {
			// Collect all consecutive tool results
			toolResults := []chat.Message{msg}
			j := i + 1
			for j < len(messages) && messages[j].Role == chat.MessageRoleTool {
				toolResults = append(toolResults, messages[j])
				j++
			}

			// Convert all tool results into content blocks
			for _, tr := range toolResults {
				var toolResultContent []types.ToolResultContentBlock
				toolResultContent = append(toolResultContent, &types.ToolResultContentBlockMemberText{
					Value: tr.Content,
				})

				contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolResult{
					Value: types.ToolResultBlock{
						ToolUseId: aws.String(tr.ToolCallID),
						Content:   toolResultContent,
					},
				})
			}

			// Skip the tool results we already processed
			i = j - 1
		} else if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) > 0 {
			// Assistant message with tool calls
			if msg.Content != "" {
				contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
					Value: msg.Content,
				})
			}

			// Add tool use blocks
			for _, tc := range msg.ToolCalls {
				// Parse tool arguments
				var input map[string]any
				if tc.Function.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						slog.Warn("Failed to unmarshal tool arguments", "tool", tc.Function.Name, "error", err)
						input = make(map[string]any)
					}
				} else {
					input = make(map[string]any)
				}

				// Convert to document type
				inputDoc, err := convertToDocument(input)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to convert tool input: %w", err)
				}

				contentBlocks = append(contentBlocks, &types.ContentBlockMemberToolUse{
					Value: types.ToolUseBlock{
						ToolUseId: aws.String(tc.ID),
						Name:      aws.String(tc.Function.Name),
						Input:     inputDoc,
					},
				})
			}
		} else {
			// Regular text message
			if msg.Content != "" {
				contentBlocks = append(contentBlocks, &types.ContentBlockMemberText{
					Value: msg.Content,
				})
			}
		}

		if len(contentBlocks) > 0 {
			converseMessages = append(converseMessages, types.Message{
				Role:    role,
				Content: contentBlocks,
			})
		}
	}

	return converseMessages, systemBlocks, nil
}

// convertToConverseTools converts cagent tools to Converse API format
func convertToConverseTools(tools []cagentTools.Tool) ([]types.Tool, error) {
	var converseTools []types.Tool

	for _, tool := range tools {
		// Convert tool parameters schema
		schemaMap, err := cagentTools.SchemaToMap(tool.Parameters)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool %s schema: %w", tool.Name, err)
		}

		// Convert schema to document
		inputSchema, err := convertToDocument(schemaMap)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool %s input schema: %w", tool.Name, err)
		}

		converseTools = append(converseTools, &types.ToolMemberToolSpec{
			Value: types.ToolSpecification{
				Name:        aws.String(tool.Name),
				Description: aws.String(tool.Description),
				InputSchema: &types.ToolInputSchemaMemberJson{
					Value: inputSchema,
				},
			},
		})
	}

	return converseTools, nil
}

// convertToDocument converts a map to AWS document type
func convertToDocument(data map[string]any) (document.Interface, error) {
	// Remove fields that Bedrock doesn't accept in JSON Schema
	cleanedData := make(map[string]any)
	for k, v := range data {
		// Skip additionalProperties as Bedrock might not accept it
		if k == "additionalProperties" {
			continue
		}
		// Convert nil "required" field to empty array, as Bedrock expects an array
		if k == "required" && v == nil {
			cleanedData[k] = []string{}
		} else {
			cleanedData[k] = v
		}
	}

	slog.Debug("Converting to document", "data", cleanedData)

	// Create lazy document from the map structure directly, not JSON bytes
	// NewLazyDocument will handle the marshaling internally
	return document.NewLazyDocument(cleanedData), nil
}
