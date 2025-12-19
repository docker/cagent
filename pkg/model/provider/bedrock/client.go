package bedrock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents a Bedrock client wrapper implementing provider.Provider
type Client struct {
	base.Config
	bedrockClient *bedrockruntime.Client
}

// bearerTokenTransport adds Authorization header with bearer token to requests
type bearerTokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

// NewClient creates a new Bedrock client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("Bedrock client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "bedrock" {
		slog.Error("Bedrock client creation failed", "error", "model type must be 'bedrock'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'bedrock'")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	// Check for Bedrock API key (simpler auth method)
	bearerToken := env.Get(ctx, "AWS_BEARER_TOKEN_BEDROCK")
	if bearerToken == "" {
		bearerToken = getProviderOpt[string](cfg.ProviderOpts, "api_key")
	}

	// Build AWS config using default credential chain
	awsCfg, err := buildAWSConfig(ctx, cfg, env)
	if err != nil {
		slog.Error("Failed to build AWS config", "error", err)
		return nil, fmt.Errorf("failed to build AWS config: %w", err)
	}

	// Create Bedrock Runtime client with appropriate auth
	var clientOpts []func(*bedrockruntime.Options)

	// Support custom endpoint for VPC endpoints or testing
	if endpoint := getProviderOpt[string](cfg.ProviderOpts, "endpoint_url"); endpoint != "" {
		clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	// If bearer token is set, use it instead of SigV4
	if bearerToken != "" {
		slog.Debug("Bedrock using API key authentication")
		clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
			// Use anonymous credentials to skip SigV4 signing
			o.Credentials = aws.AnonymousCredentials{}
			// Add bearer token via custom HTTP client
			o.HTTPClient = &http.Client{
				Transport: &bearerTokenTransport{
					token: bearerToken,
					base:  http.DefaultTransport,
				},
			}
		})
	}

	bedrockClient := bedrockruntime.NewFromConfig(awsCfg, clientOpts...)

	slog.Debug("Bedrock client created successfully", "model", cfg.Model, "region", awsCfg.Region)

	return &Client{
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
			Env:          env,
		},
		bedrockClient: bedrockClient,
	}, nil
}

// buildAWSConfig creates AWS config with proper credentials using the default credential chain
func buildAWSConfig(ctx context.Context, cfg *latest.ModelConfig, env environment.Provider) (aws.Config, error) {
	var configOpts []func(*config.LoadOptions) error

	// Region from provider_opts or environment
	region := getProviderOpt[string](cfg.ProviderOpts, "region")
	if region == "" {
		region = env.Get(ctx, "AWS_REGION")
	}
	if region == "" {
		region = env.Get(ctx, "AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1" // Default region
	}
	configOpts = append(configOpts, config.WithRegion(region))

	// Profile from provider_opts
	if profile := getProviderOpt[string](cfg.ProviderOpts, "profile"); profile != "" {
		configOpts = append(configOpts, config.WithSharedConfigProfile(profile))
	}

	// Load base config with default credential chain
	awsCfg, err := config.LoadDefaultConfig(ctx, configOpts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Handle assume role if specified
	if roleARN := getProviderOpt[string](cfg.ProviderOpts, "role_arn"); roleARN != "" {
		stsClient := sts.NewFromConfig(awsCfg)
		creds := stscreds.NewAssumeRoleProvider(stsClient, roleARN, func(o *stscreds.AssumeRoleOptions) {
			if sessionName := getProviderOpt[string](cfg.ProviderOpts, "role_session_name"); sessionName != "" {
				o.RoleSessionName = sessionName
			} else {
				o.RoleSessionName = "cagent-bedrock-session"
			}
			if externalID := getProviderOpt[string](cfg.ProviderOpts, "external_id"); externalID != "" {
				o.ExternalID = aws.String(externalID)
			}
		})
		awsCfg.Credentials = aws.NewCredentialsCache(creds)
		slog.Debug("Bedrock using assumed role", "role_arn", roleARN)
	}

	return awsCfg, nil
}

// CreateChatCompletionStream creates a streaming chat completion request
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	messages []chat.Message,
	requestTools []tools.Tool,
) (chat.MessageStream, error) {
	slog.Debug("Creating Bedrock chat completion stream",
		"model", c.ModelConfig.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools))

	if len(messages) == 0 {
		return nil, errors.New("at least one message is required")
	}

	// Build Converse input
	input := c.buildConverseStreamInput(messages, requestTools)

	// Call ConverseStream
	output, err := c.bedrockClient.ConverseStream(ctx, input)
	if err != nil {
		slog.Error("Bedrock ConverseStream failed", "error", err)
		return nil, fmt.Errorf("bedrock converse stream failed: %w", err)
	}

	trackUsage := c.ModelConfig.TrackUsage == nil || *c.ModelConfig.TrackUsage
	return newStreamAdapter(output.GetStream(), c.ModelConfig.Model, trackUsage), nil
}

// buildConverseStreamInput creates the ConverseStream input parameters
func (c *Client) buildConverseStreamInput(messages []chat.Message, requestTools []tools.Tool) *bedrockruntime.ConverseStreamInput {
	input := &bedrockruntime.ConverseStreamInput{
		ModelId: aws.String(c.ModelConfig.Model),
	}

	// Convert and set messages (excluding system)
	input.Messages, input.System = convertMessages(messages)

	// Set inference configuration
	input.InferenceConfig = c.buildInferenceConfig()

	// Convert and set tools
	if len(requestTools) > 0 {
		input.ToolConfig = convertToolConfig(requestTools)
	}

	return input
}

// buildInferenceConfig creates the inference configuration
func (c *Client) buildInferenceConfig() *types.InferenceConfiguration {
	cfg := &types.InferenceConfiguration{}

	if c.ModelConfig.MaxTokens > 0 {
		cfg.MaxTokens = aws.Int32(int32(c.ModelConfig.MaxTokens))
	}
	if c.ModelConfig.Temperature != nil {
		cfg.Temperature = aws.Float32(float32(*c.ModelConfig.Temperature))
	}
	if c.ModelConfig.TopP != nil {
		cfg.TopP = aws.Float32(float32(*c.ModelConfig.TopP))
	}

	return cfg
}

// getProviderOpt extracts a typed value from provider_opts
func getProviderOpt[T any](opts map[string]any, key string) T {
	var zero T
	if opts == nil {
		return zero
	}
	v, ok := opts[key]
	if !ok {
		return zero
	}
	typed, ok := v.(T)
	if !ok {
		return zero
	}
	return typed
}
