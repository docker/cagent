package dmr

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
	"golang.org/x/term"

	"github.com/docker/cagent/pkg/chat"
	"github.com/docker/cagent/pkg/config/latest"
	"github.com/docker/cagent/pkg/input"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/oaistream"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/rag/types"
	"github.com/docker/cagent/pkg/tools"
)

const (
	// configureTimeout is the timeout for the model configure HTTP request.
	// This is kept short to avoid stalling client creation.
	configureTimeout = 10 * time.Second

	// connectivityTimeout is the timeout for testing DMR endpoint connectivity.
	// This is kept short to quickly detect unreachable endpoints and try fallbacks.
	connectivityTimeout = 2 * time.Second
)

// ErrNotInstalled is returned when Docker Model Runner is not installed.
var ErrNotInstalled = errors.New("docker model runner is not available\nplease install it and try again (https://docs.docker.com/ai/model-runner/get-started/)")

const (
	// dmrInferencePrefix mirrors github.com/docker/model-runner/pkg/inference.InferencePrefix.
	dmrInferencePrefix = "/engines"
	// dmrExperimentalEndpointsPrefix mirrors github.com/docker/model-runner/pkg/inference.ExperimentalEndpointsPrefix.
	dmrExperimentalEndpointsPrefix = "/exp/vDD4.40"

	// dmrDefaultPort is the default port for Docker Model Runner.
	dmrDefaultPort = "12434"
)

// Client represents an DMR client wrapper
// It implements the provider.Provider interface
type Client struct {
	base.Config
	client     openai.Client
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new DMR client from the provided configuration
func NewClient(ctx context.Context, cfg *latest.ModelConfig, opts ...options.Opt) (*Client, error) {
	if cfg == nil {
		slog.Error("DMR client creation failed", "error", "model configuration is required")
		return nil, errors.New("model configuration is required")
	}

	if cfg.Provider != "dmr" {
		slog.Error("DMR client creation failed", "error", "model type must be 'dmr'", "actual_type", cfg.Provider)
		return nil, errors.New("model type must be 'dmr'")
	}

	var globalOptions options.ModelOptions
	for _, opt := range opts {
		opt(&globalOptions)
	}

	// Skip docker model status query when BaseURL is explicitly provided.
	// This avoids unnecessary exec calls and speeds up tests/CI scenarios.
	var endpoint, engine string
	if cfg.BaseURL == "" && os.Getenv("MODEL_RUNNER_HOST") == "" {
		var err error
		endpoint, engine, err = getDockerModelEndpointAndEngine(ctx)
		if err != nil {
			if err.Error() == "unknown flag: --json\n\nUsage:  docker [OPTIONS] COMMAND [ARG...]\n\nRun 'docker --help' for more information" {
				slog.Debug("docker model status query failed", "error", err)
				return nil, ErrNotInstalled
			}
			slog.Error("docker model status query failed", "error", err)
		} else {
			// Auto-pull the model if needed
			if err := pullDockerModelIfNeeded(ctx, cfg.Model); err != nil {
				slog.Debug("docker model pull failed", "error", err)
				return nil, err
			}
		}
	}

	baseURL, clientOptions, httpClient := resolveDMRBaseURL(ctx, cfg, endpoint)

	// Ensure we always have a non-nil HTTP client for both OpenAI adapter and direct HTTP calls (rerank).
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	clientOptions = append(clientOptions, option.WithBaseURL(baseURL), option.WithAPIKey("")) // DMR doesn't need auth

	// Build runtime flags from ModelConfig and engine
	contextSize, providerRuntimeFlags, specOpts := parseDMRProviderOpts(cfg)
	configFlags := buildRuntimeFlagsFromModelConfig(engine, cfg)
	finalFlags, warnings := mergeRuntimeFlagsPreferUser(configFlags, providerRuntimeFlags)
	for _, w := range warnings {
		slog.Warn(w)
	}
	slog.Debug("DMR provider_opts parsed", "model", cfg.Model, "context_size", contextSize, "runtime_flags", finalFlags, "speculative_opts", specOpts, "engine", engine)
	// Skip model configuration when generating titles to avoid reconfiguring the model
	// with different settings (e.g., smaller max_tokens) that would affect the main agent.
	if !globalOptions.GeneratingTitle() {
		if err := configureModel(ctx, httpClient, baseURL, cfg.Model, contextSize, finalFlags, specOpts); err != nil {
			slog.Debug("model configure via API skipped or failed", "error", err)
		}
	}

	slog.Debug("DMR client created successfully", "model", cfg.Model, "base_url", baseURL)

	return &Client{
		Config: base.Config{
			ModelConfig:  *cfg,
			ModelOptions: globalOptions,
		},
		client:     openai.NewClient(clientOptions...),
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func inContainer() bool {
	finfo, err := os.Stat("/.dockerenv")
	return err == nil && finfo.Mode().IsRegular()
}

// testDMRConnectivity performs a quick health check against a DMR endpoint.
// It returns true if the endpoint is reachable and responds within the timeout.
func testDMRConnectivity(ctx context.Context, httpClient *http.Client, baseURL string) bool {
	// Build a simple health check URL - try the models endpoint which should always exist
	healthURL := strings.TrimSuffix(baseURL, "/") + "/models"

	ctx, cancel := context.WithTimeout(ctx, connectivityTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, http.NoBody)
	if err != nil {
		slog.Debug("DMR connectivity check: failed to create request", "url", healthURL, "error", err)
		return false
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Debug("DMR connectivity check: request failed", "url", healthURL, "error", err)
		return false
	}
	defer resp.Body.Close()

	// Any response (even 4xx/5xx) means the server is reachable
	slog.Debug("DMR connectivity check: success", "url", healthURL, "status", resp.StatusCode)
	return true
}

// getDMRFallbackURLs returns a list of fallback URLs to try for DMR connectivity.
// The order is chosen to maximize compatibility across platforms:
// 1. model-runner.docker.internal - Docker Desktop's integrated model-runner
// 2. host.docker.internal - Docker Desktop's host access (works on macOS/Windows/Linux Desktop)
// 3. 172.17.0.1 - Default Docker bridge gateway (Linux Docker CE)
// 4. 127.0.0.1 - Localhost (when running directly on host)
func getDMRFallbackURLs(containerized bool) []string {
	// Docker Desktop internal hostnames and fallback IPs for cross-platform support.
	// These are tried in order when the primary endpoint is unreachable.
	// The fallback URLs differ based on whether we're running inside a container or on the host.
	const dmrModelRunnerInternal = "model-runner.docker.internal" // Docker Desktop's model-runner service (container only)
	const dmrHostDockerInternal = "host.docker.internal"          // Docker Desktop's host access (container only)
	const dmrDockerBridgeGateway = "172.17.0.1"                   // Default Docker bridge gateway (container on Linux Docker CE only)
	const dmrLocalhost = "127.0.0.1"                              // Localhost fallback (host only)

	if containerized {
		// Inside a container: try Docker internal hostnames and bridge gateway
		return []string{
			fmt.Sprintf("http://%s%s/v1/", dmrModelRunnerInternal, dmrInferencePrefix),
			fmt.Sprintf("http://%s:%s%s/v1/", dmrHostDockerInternal, dmrDefaultPort, dmrInferencePrefix),
			fmt.Sprintf("http://%s:%s%s/v1/", dmrDockerBridgeGateway, dmrDefaultPort, dmrInferencePrefix),
		}
	}
	// On the host: only localhost makes sense as a fallback
	return []string{
		fmt.Sprintf("http://%s:%s%s/v1/", dmrLocalhost, dmrDefaultPort, dmrInferencePrefix),
	}
}

// resolveDMRBaseURL determines the correct base URL and HTTP options to talk to
// Docker Model Runner, mirroring the behavior of the `docker model` CLI as
// closely as possible.
//
// High‑level rules:
//   - If the user explicitly configured a BaseURL or MODEL_RUNNER_HOST, use that (no fallbacks).
//   - For Desktop endpoints (model-runner.docker.internal) on the host, route
//     through the Docker Engine experimental endpoints prefix over the Unix socket.
//   - For standalone / offload endpoints like http://172.17.0.1:12435/engines/v1/,
//     use localhost:<port>/engines/v1/ on the host, and the gateway IP:port inside containers.
//   - Keep a small compatibility workaround for the legacy http://:0/engines/v1/ endpoint.
//   - Test connectivity and try fallback URLs if the primary endpoint is unreachable.
//
// It also returns an *http.Client when a custom transport (e.g., Docker Unix socket) is needed.
func resolveDMRBaseURL(ctx context.Context, cfg *latest.ModelConfig, endpoint string) (string, []option.RequestOption, *http.Client) {
	// Explicit configuration - return immediately without fallback testing
	if cfg != nil && cfg.BaseURL != "" {
		slog.Debug("DMR using explicitly configured BaseURL", "url", cfg.BaseURL)
		return cfg.BaseURL, nil, nil
	}
	if host := os.Getenv("MODEL_RUNNER_HOST"); host != "" {
		trimmed := strings.TrimRight(host, "/")
		baseURL := trimmed + dmrInferencePrefix + "/v1/"
		slog.Debug("DMR using MODEL_RUNNER_HOST", "url", baseURL)
		return baseURL, nil, nil
	}

	// Resolve primary URL based on endpoint
	baseURL, clientOptions, httpClient := resolvePrimaryDMRURL(endpoint)

	// Test connectivity and try fallbacks if needed
	testClient := httpClient
	if testClient == nil {
		testClient = &http.Client{}
	}

	containerized := inContainer()

	if !testDMRConnectivity(ctx, testClient, baseURL) {
		slog.Debug("DMR primary endpoint unreachable, trying fallbacks", "primary_url", baseURL, "in_container", containerized)

		for _, fallbackURL := range getDMRFallbackURLs(containerized) {
			if fallbackURL == baseURL {
				continue // Skip if same as primary
			}
			slog.Debug("DMR trying fallback endpoint", "url", fallbackURL)
			if testDMRConnectivity(ctx, &http.Client{}, fallbackURL) {
				slog.Info("DMR using fallback endpoint", "fallback_url", fallbackURL, "original_url", baseURL)
				// Reset client options since we're using a different URL (no Unix socket needed for HTTP endpoints)
				return fallbackURL, nil, nil
			}
		}
		// All endpoints unreachable - log warning but continue with primary URL.
		// The client will fail on first actual use, providing a better error at that point.
		// This allows users to still start the TUI, change sessions, provider/model, etc even if connections to DMR fail
		slog.Error("DMR all endpoints currently unreachable, will fail on first use", "primary_url", baseURL, "in_container", containerized)
	} else {
		slog.Debug("DMR primary endpoint reachable", "url", baseURL)
	}

	return baseURL, clientOptions, httpClient
}

// resolvePrimaryDMRURL resolves the primary DMR URL based on the endpoint string.
// This handles the various endpoint formats and platform-specific routing without
// connectivity testing or fallbacks.
func resolvePrimaryDMRURL(endpoint string) (string, []option.RequestOption, *http.Client) {
	var clientOptions []option.RequestOption
	var httpClient *http.Client

	ep := strings.TrimSpace(endpoint)

	// Legacy bug workaround: old DMR versions <= 0.1.44 could report http://:0/engines/v1/.
	if ep == "http://:0/engines/v1/" {
		// Use the default port on localhost.
		return "http://127.0.0.1:12434" + dmrInferencePrefix + "/v1/", clientOptions, httpClient
	}

	// If we don't have a usable endpoint, fall back to sensible defaults.
	if ep == "" {
		if inContainer() {
			// In a container with no endpoint info, assume Docker Desktop's internal host.
			return "http://model-runner.docker.internal" + dmrInferencePrefix + "/v1/", clientOptions, httpClient
		}
		// On the host with no endpoint info: default to the standard local Moby port.
		return "http://127.0.0.1:12434" + dmrInferencePrefix + "/v1/", clientOptions, httpClient
	}

	u, err := url.Parse(ep)
	if err != nil {
		slog.Debug("failed to parse DMR endpoint, falling back to defaults", "endpoint", ep, "error", err)
		if inContainer() {
			return "http://model-runner.docker.internal" + dmrInferencePrefix + "/v1/", clientOptions, httpClient
		}
		return "http://127.0.0.1:12434" + dmrInferencePrefix + "/v1/", clientOptions, httpClient
	}

	host := u.Hostname()
	port := u.Port()

	if host == "model-runner.docker.internal" && !inContainer() {
		// Build "http://_/exp/vDD4.40/engines/v1" using the shared experimental prefix.
		expPrefix := strings.TrimPrefix(dmrExperimentalEndpointsPrefix, "/")
		baseURL := fmt.Sprintf("http://_/%s%s/v1", expPrefix, dmrInferencePrefix)

		httpClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", "/var/run/docker.sock")
				},
			},
		}

		clientOptions = append(clientOptions, option.WithHTTPClient(httpClient))

		return baseURL, clientOptions, httpClient
	}

	// Default port when the status output omits it.
	port = cmp.Or(port, "12434")

	if inContainer() {
		baseURL := ep
		if !strings.HasSuffix(baseURL, "/") {
			baseURL += "/"
		}
		return baseURL, clientOptions, httpClient
	}

	// Host case – always talk to localhost:<port>/engines/v1/, even if the status
	// endpoint uses a gateway IP like 172.17.0.1.
	baseURL := fmt.Sprintf("http://127.0.0.1:%s%s/v1/", port, dmrInferencePrefix)
	return baseURL, clientOptions, httpClient
}

// mergeRuntimeFlagsPreferUser merges derived engine flags (the ones under `models:` e.g. `temperature`)
// and user-provided runtime flags (the ones under `models:provider_opts:runtime_flags:`).
// If both specify the same flag key (e.g., --temp), the `models:provider_opts:runtime_flags:` value is preferred and a warning is returned.
// The result preserves order: all non-conflicting derived flags first, followed by all user flags.
func mergeRuntimeFlagsPreferUser(derived, user []string) (out, warnings []string) {
	type flagKV struct {
		key  string
		val  *string
		orig []string
	}
	parse := func(tokens []string) []flagKV {
		var out []flagKV
		for i := 0; i < len(tokens); i++ {
			tok := tokens[i]
			if strings.HasPrefix(tok, "-") {
				// handle --key=value or -k=value
				if k, v, found := strings.Cut(tok, "="); found {
					out = append(out, flagKV{key: k, val: &v, orig: []string{tok}})
					continue
				}
				// handle spaced value: --key value
				if i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
					v := tokens[i+1]
					out = append(out, flagKV{key: tok, val: &v, orig: []string{tok, v}})
					i++
				} else {
					// boolean/flag without value
					out = append(out, flagKV{key: tok, val: nil, orig: []string{tok}})
				}
			} else {
				// orphan value; keep as-is
				v := tok
				out = append(out, flagKV{key: tok, val: &v, orig: []string{tok}})
			}
		}
		return out
	}
	der := parse(derived)
	usr := parse(user)

	// Index derived by key
	derivedIdx := map[string]int{}
	for i, kv := range der {
		// Only index proper flags (start with '-') to avoid colliding with orphan values
		if strings.HasPrefix(kv.key, "-") {
			derivedIdx[kv.key] = i
		}
	}

	conflicts := map[string]bool{}
	for _, kv := range usr {
		if strings.HasPrefix(kv.key, "-") {
			if _, ok := derivedIdx[kv.key]; ok {
				conflicts[kv.key] = true
			}
		}
	}

	for i, kv := range der {
		if strings.HasPrefix(kv.key, "-") && conflicts[kv.key] {
			warnings = append(warnings, "Overriding runtime flag "+kv.key+" with value from provider_opts.runtime_flags")
			continue // skip derived conflicting flag
		}
		out = append(out, kv.orig...)
		// also append any non-flag or orphan tokens (they are already in kv.orig)
		_ = i
	}
	// Append all user flags at the end
	for _, kv := range usr {
		out = append(out, kv.orig...)
	}
	return out, warnings
}

// convertMessages converts chat messages to OpenAI format and merges consecutive
// system/user messages, which is needed by some local models run by DMR.
func convertMessages(messages []chat.Message) []openai.ChatCompletionMessageParamUnion {
	openaiMessages := oaistream.ConvertMessages(messages)
	return oaistream.MergeConsecutiveMessages(openaiMessages)
}

// CreateChatCompletionStream creates a streaming chat completion request
// It returns a stream that can be iterated over to get completion chunks
func (c *Client) CreateChatCompletionStream(ctx context.Context, messages []chat.Message, requestTools []tools.Tool) (chat.MessageStream, error) {
	slog.Debug("Creating DMR chat completion stream",
		"model", c.ModelConfig.Model,
		"message_count", len(messages),
		"tool_count", len(requestTools),
		"base_url", c.baseURL,
	)

	if len(messages) == 0 {
		slog.Error("DMR stream creation failed", "error", "at least one message is required")
		return nil, errors.New("at least one message is required")
	}

	trackUsage := c.ModelConfig.TrackUsage == nil || *c.ModelConfig.TrackUsage

	params := openai.ChatCompletionNewParams{
		Model:    c.ModelConfig.Model,
		Messages: convertMessages(messages),
		StreamOptions: openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: openai.Bool(trackUsage),
		},
	}

	if c.ModelConfig.Temperature != nil {
		params.Temperature = openai.Float(*c.ModelConfig.Temperature)
	}
	if c.ModelConfig.TopP != nil {
		params.TopP = openai.Float(*c.ModelConfig.TopP)
	}
	if c.ModelConfig.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(*c.ModelConfig.FrequencyPenalty)
	}
	if c.ModelConfig.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(*c.ModelConfig.PresencePenalty)
	}

	// Only set ParallelToolCalls when tools are present; matches OpenAI provider behavior
	if len(requestTools) > 0 && c.ModelConfig.ParallelToolCalls != nil {
		params.ParallelToolCalls = openai.Bool(*c.ModelConfig.ParallelToolCalls)
	}

	if c.ModelConfig.MaxTokens != nil {
		params.MaxTokens = openai.Int(*c.ModelConfig.MaxTokens)
		slog.Debug("DMR request configured with max tokens", "max_tokens", *c.ModelConfig.MaxTokens)
	}

	if len(requestTools) > 0 {
		slog.Debug("Adding tools to DMR request", "tool_count", len(requestTools))
		toolsParam := make([]openai.ChatCompletionToolUnionParam, len(requestTools))
		for i, tool := range requestTools {
			// DMR requires the `description` key to be present; ensure a non-empty value
			// NOTE(krissetto): workaround, remove when fixed upstream, this shouldn't be necceessary
			desc := cmp.Or(tool.Description, "Function "+tool.Name)

			parameters, err := ConvertParametersToSchema(tool.Parameters)
			if err != nil {
				slog.Error("Failed to convert tool parameters to DMR schema", "error", err, "tool", tool.Name)
				return nil, fmt.Errorf("failed to convert tool parameters to DMR schema for tool %s: %w", tool.Name, err)
			}

			paramsMap, ok := parameters.(map[string]any)
			if !ok {
				slog.Error("Converted parameters is not a map", "tool", tool.Name)
				return nil, fmt.Errorf("converted parameters is not a map for tool %s", tool.Name)
			}

			toolsParam[i] = openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        tool.Name,
				Description: openai.String(desc),
				Parameters:  paramsMap,
			})
		}
		params.Tools = toolsParam

		if c.ModelConfig.ParallelToolCalls != nil {
			params.ParallelToolCalls = openai.Bool(*c.ModelConfig.ParallelToolCalls)
		}
	}

	// Log the request in JSON format for debugging
	if requestJSON, err := json.Marshal(params); err == nil {
		slog.Debug("DMR chat completion request", "request", string(requestJSON))
	} else {
		slog.Error("Failed to marshal DMR request to JSON", "error", err)
	}

	if structuredOutput := c.ModelOptions.StructuredOutput(); structuredOutput != nil {
		slog.Debug("Adding structured output to DMR request", "structured_output", structuredOutput)

		params.ResponseFormat.OfJSONSchema = &openai.ResponseFormatJSONSchemaParam{
			JSONSchema: openai.ResponseFormatJSONSchemaJSONSchemaParam{
				Name:        structuredOutput.Name,
				Description: openai.String(structuredOutput.Description),
				Schema:      jsonSchema(structuredOutput.Schema),
				Strict:      openai.Bool(structuredOutput.Strict),
			},
		}
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)

	slog.Debug("DMR chat completion stream created successfully", "model", c.ModelConfig.Model, "base_url", c.baseURL)
	return newStreamAdapter(stream, trackUsage), nil
}

// CreateEmbedding generates an embedding vector for the given text with usage tracking.
func (c *Client) CreateEmbedding(ctx context.Context, text string) (*base.EmbeddingResult, error) {
	slog.Debug("Creating DMR embedding", "model", c.ModelConfig.Model, "text_length", len(text), "base_url", c.baseURL)

	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: []string{text},
		},
		Model: c.ModelConfig.Model,
	}

	response, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		slog.Error("DMR embedding request failed", "error", err)
		return nil, fmt.Errorf("failed to create embedding: %w", err)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned from DMR")
	}

	// Convert []float32 to []float64
	embedding32 := response.Data[0].Embedding
	embedding := make([]float64, len(embedding32))
	copy(embedding, embedding32)

	// Extract usage information
	inputTokens := response.Usage.PromptTokens
	totalTokens := response.Usage.TotalTokens

	// DMR is local/free, so cost is 0
	cost := 0.0

	slog.Debug("DMR embedding created successfully",
		"dimension", len(embedding),
		"input_tokens", inputTokens,
		"total_tokens", totalTokens)

	return &base.EmbeddingResult{
		Embedding:   embedding,
		InputTokens: inputTokens,
		TotalTokens: totalTokens,
		Cost:        cost,
	}, nil
}

// CreateBatchEmbedding generates embedding vectors for multiple texts with usage tracking.
func (c *Client) CreateBatchEmbedding(ctx context.Context, texts []string) (*base.BatchEmbeddingResult, error) {
	if len(texts) == 0 {
		return &base.BatchEmbeddingResult{
			Embeddings: [][]float64{},
		}, nil
	}

	slog.Debug("Creating DMR batch embeddings", "model", c.ModelConfig.Model, "batch_size", len(texts), "base_url", c.baseURL)

	params := openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{
			OfArrayOfStrings: texts,
		},
		Model: c.ModelConfig.Model,
	}

	response, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		slog.Error("DMR batch embedding request failed", "error", err)
		return nil, fmt.Errorf("failed to create batch embeddings: %w", err)
	}

	if len(response.Data) != len(texts) {
		return nil, fmt.Errorf("expected %d embeddings, got %d", len(texts), len(response.Data))
	}

	// Convert embeddings from []float32 to [][]float64
	embeddings := make([][]float64, len(response.Data))
	for i, data := range response.Data {
		embedding32 := data.Embedding
		embedding := make([]float64, len(embedding32))
		copy(embedding, embedding32)
		embeddings[i] = embedding
	}

	// Extract usage information
	inputTokens := response.Usage.PromptTokens
	totalTokens := response.Usage.TotalTokens

	// DMR is local/free, so cost is 0
	cost := 0.0

	slog.Debug("DMR batch embeddings created successfully",
		"batch_size", len(embeddings),
		"dimension", len(embeddings[0]),
		"input_tokens", inputTokens,
		"total_tokens", totalTokens)

	return &base.BatchEmbeddingResult{
		Embeddings:  embeddings,
		InputTokens: inputTokens,
		TotalTokens: totalTokens,
		Cost:        cost,
	}, nil
}

// Rerank scores documents by relevance to the query using a reranking model.
// Returns relevance scores in the same order as input documents.
func (c *Client) Rerank(ctx context.Context, query string, documents []types.Document, criteria string) ([]float64, error) {
	startTime := time.Now()

	if len(documents) == 0 {
		return []float64{}, nil
	}

	// DMR uses a native /rerank endpoint that doesn't support custom criteria or metadata
	// Log a warning if criteria was provided but cannot be used
	if criteria != "" {
		slog.Warn("DMR reranking does not support custom criteria",
			"model", c.ModelConfig.Model,
			"criteria_length", len(criteria))
	}

	// Extract content strings from Document structs for DMR native endpoint
	// The native endpoint only accepts string content, not metadata
	documentStrings := make([]string, len(documents))
	totalDocLength := 0
	for i, doc := range documents {
		documentStrings[i] = doc.Content
		totalDocLength += len(doc.Content)
	}

	// Extract base URL without /engines/v1/ suffix for logging and URL construction
	parsedURL, parseErr := url.Parse(c.baseURL)
	if parseErr != nil {
		slog.Error("DMR rerank invalid base URL", "base_url", c.baseURL, "error", parseErr)
		return nil, fmt.Errorf("invalid base URL: %w", parseErr)
	}

	scheme := cmp.Or(parsedURL.Scheme, "http")
	host := cmp.Or(parsedURL.Host, "127.0.0.1:12434")

	baseURL := fmt.Sprintf("%s://%s", scheme, host)

	slog.Debug("DMR reranking request",
		"model", c.ModelConfig.Model,
		"base_url", baseURL,
		"query_length", len(query),
		"num_documents", len(documents),
		"total_doc_length", totalDocLength,
		"avg_doc_length", totalDocLength/len(documents))

	// Prepare rerank request
	type rerankRequest struct {
		Model     string   `json:"model"`
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
	}

	type rerankResponse struct {
		Results []struct {
			Index          int     `json:"index"`
			RelevanceScore float64 `json:"relevance_score"`
		} `json:"results"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	}

	reqBody := rerankRequest{
		Model:     c.ModelConfig.Model,
		Query:     query,
		Documents: documentStrings,
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		slog.Error("DMR rerank request marshaling failed", "error", err)
		return nil, fmt.Errorf("failed to marshal rerank request: %w", err)
	}

	// Make HTTP request to rerank endpoint
	// Rerank endpoint is at the base host level: http://host:port/rerank
	// Not under /engines/v1/ like chat/embeddings
	rerankURL := fmt.Sprintf("%s/rerank", baseURL)

	slog.Debug("DMR reranking HTTP request",
		"base_url", baseURL,
		"rerank_url", rerankURL,
		"method", http.MethodPost,
		"payload_size", len(reqData))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rerankURL, bytes.NewReader(reqData))
	if err != nil {
		slog.Error("DMR rerank request creation failed", "url", rerankURL, "error", err)
		return nil, fmt.Errorf("failed to create rerank request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("DMR rerank HTTP request failed",
			"base_url", baseURL,
			"rerank_url", rerankURL,
			"error", err)
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("DMR rerank HTTP response",
		"base_url", baseURL,
		"status_code", resp.StatusCode,
		"status", resp.Status)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("DMR rerank request failed",
			"base_url", baseURL,
			"rerank_url", rerankURL,
			"status_code", resp.StatusCode,
			"status", resp.Status,
			"response_body", string(body))
		return nil, fmt.Errorf("rerank request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var rerankResp rerankResponse
	if err := json.NewDecoder(resp.Body).Decode(&rerankResp); err != nil {
		slog.Error("DMR rerank response decoding failed",
			"model", c.ModelConfig.Model,
			"base_url", baseURL,
			"error", err)
		return nil, fmt.Errorf("failed to decode rerank response: %w", err)
	}

	if len(rerankResp.Results) != len(documents) {
		slog.Error("DMR rerank result count mismatch",
			"model", c.ModelConfig.Model,
			"base_url", baseURL,
			"expected", len(documents),
			"received", len(rerankResp.Results))
		return nil, fmt.Errorf("expected %d rerank scores, got %d", len(documents), len(rerankResp.Results))
	}

	// Extract scores in order
	scores := make([]float64, len(documents))
	var minScore, maxScore float64
	firstScore := true

	for _, result := range rerankResp.Results {
		if result.Index < 0 || result.Index >= len(documents) {
			slog.Error("DMR rerank invalid result index",
				"model", c.ModelConfig.Model,
				"base_url", baseURL,
				"index", result.Index,
				"max_valid_index", len(documents)-1)
			return nil, fmt.Errorf("invalid result index %d", result.Index)
		}
		scores[result.Index] = result.RelevanceScore

		// Track score statistics (raw logits)
		if firstScore {
			minScore = result.RelevanceScore
			maxScore = result.RelevanceScore
			firstScore = false
		} else {
			minScore = min(minScore, result.RelevanceScore)
			maxScore = max(maxScore, result.RelevanceScore)
		}
	}

	// Log raw logit statistics
	rawSum := 0.0
	for _, s := range scores {
		rawSum += s
	}
	rawAvgScore := rawSum / float64(len(scores))

	slog.Debug("DMR reranking raw logits",
		"model", c.ModelConfig.Model,
		"base_url", baseURL,
		"raw_min_score", minScore,
		"raw_max_score", maxScore,
		"raw_avg_score", rawAvgScore)

	// Normalize scores to [0, 1] range using sigmoid normalization
	// Sigmoid: 1 / (1 + exp(-x)) preserves absolute magnitude information
	// Unlike min-max, this allows thresholds to work consistently across queries:
	// - Positive logits → 0.5 to 1.0 (relevant)
	// - Zero logit → 0.5 (neutral)
	// - Negative logits → 0.0 to 0.5 (irrelevant)
	// This matches the behavior of OpenAI/Anthropic which use LLMs to generate [0,1] scores
	if len(scores) > 0 {
		for i := range scores {
			scores[i] = sigmoid(scores[i])
		}
		slog.Debug("DMR reranking normalized scores using sigmoid",
			"model", c.ModelConfig.Model,
			"base_url", baseURL,
			"normalization", "sigmoid")
	}

	// Calculate normalized statistics
	sumScore := 0.0
	normalizedMin := 1.0
	normalizedMax := 0.0
	for _, s := range scores {
		sumScore += s
		normalizedMin = min(normalizedMin, s)
		normalizedMax = max(normalizedMax, s)
	}
	avgScore := sumScore / float64(len(scores))

	totalDuration := time.Since(startTime)

	slog.Debug("DMR reranking complete",
		"model", c.ModelConfig.Model,
		"base_url", baseURL,
		"num_scores", len(scores),
		"normalized_min_score", normalizedMin,
		"normalized_max_score", normalizedMax,
		"normalized_avg_score", avgScore,
		"prompt_tokens", rerankResp.Usage.PromptTokens,
		"total_tokens", rerankResp.Usage.TotalTokens,
		"duration_ms", totalDuration.Milliseconds())

	// Log top 3 normalized scores for debugging
	if len(scores) > 0 {
		slog.Debug("DMR rerank top normalized scores",
			"top_1", scores[0],
			"top_2", func() float64 {
				if len(scores) > 1 {
					return scores[1]
				}
				return 0
			}(),
			"top_3", func() float64 {
				if len(scores) > 2 {
					return scores[2]
				}
				return 0
			}())
	}

	return scores, nil
}

// ConvertParametersToSchema converts parameters to DMR Schema format
func ConvertParametersToSchema(params any) (any, error) {
	m, err := tools.SchemaToMap(params)
	if err != nil {
		return nil, err
	}

	// DMR models tend to dislike `additionalProperties` in the schema
	// e.g. ai/qwen3 and ai/gpt-oss
	delete(m, "additionalProperties")

	return m, nil
}

type speculativeDecodingOpts struct {
	draftModel     string
	numTokens      int
	acceptanceRate float64
}

// parseFloat64 attempts to parse a value as float64 from various types.
// Returns the parsed value and true if successful, otherwise 0 and false.
func parseFloat64(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint64:
		return float64(t), true
	case string:
		if s := strings.TrimSpace(t); s != "" {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// parseInt attempts to parse a value as int from various types.
// Returns the parsed value and true if successful, otherwise 0 and false.
func parseInt(v any) (int, bool) {
	if f, ok := parseFloat64(v); ok {
		return int(f), true
	}
	return 0, false
}

func parseDMRProviderOpts(cfg *latest.ModelConfig) (contextSize *int64, runtimeFlags []string, specOpts *speculativeDecodingOpts) {
	if cfg == nil {
		return nil, nil, nil
	}

	// Context length is now sourced from the standard max_tokens field
	contextSize = cfg.MaxTokens

	slog.Debug("DMR provider opts", "provider_opts", cfg.ProviderOpts)

	if len(cfg.ProviderOpts) == 0 {
		return contextSize, runtimeFlags, specOpts
	}

	// Parse runtime flags
	if v, ok := cfg.ProviderOpts["runtime_flags"]; ok {
		switch t := v.(type) {
		case []any:
			for _, item := range t {
				runtimeFlags = append(runtimeFlags, fmt.Sprint(item))
			}
		case []string:
			runtimeFlags = append(runtimeFlags, t...)
		case string:
			parts := strings.Fields(strings.ReplaceAll(t, ",", " "))
			runtimeFlags = append(runtimeFlags, parts...)
		}
	}

	// Parse speculative decoding options using helper functions
	var opts speculativeDecodingOpts
	var hasOpts bool

	if v, ok := cfg.ProviderOpts["speculative_draft_model"]; ok {
		if s, ok := v.(string); ok && s != "" {
			opts.draftModel = s
			hasOpts = true
		}
	}

	if v, ok := cfg.ProviderOpts["speculative_num_tokens"]; ok {
		if n, ok := parseInt(v); ok {
			opts.numTokens = n
			hasOpts = true
		}
	}

	if v, ok := cfg.ProviderOpts["speculative_acceptance_rate"]; ok {
		if f, ok := parseFloat64(v); ok {
			opts.acceptanceRate = f
			hasOpts = true
		}
	}

	if hasOpts {
		specOpts = &opts
	}

	return contextSize, runtimeFlags, specOpts
}

func pullDockerModelIfNeeded(ctx context.Context, model string) error {
	// Check if running in interactive mode (stdin is a terminal)
	interactive := term.IsTerminal(int(os.Stdin.Fd()))
	if !interactive {
		// In non-interactive mode (CI / Servers), do not attempt to pull the model
		return nil
	}

	if modelExists(ctx, model) {
		slog.Debug("Model already exists, skipping pull", "model", model)
		return nil
	}

	// Prompt user for confirmation in interactive mode
	fmt.Printf("\nModel %s not found locally.\n", model)
	fmt.Printf("Do you want to pull it now? ([y]es/[n]o): ")

	response, err := input.ReadLine(ctx, os.Stdin)
	if err != nil {
		return fmt.Errorf("failed to read user input: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		return fmt.Errorf("model pull declined by user")
	}

	// Pull the model
	slog.Info("Pulling DMR model", "model", model)
	fmt.Printf("Pulling model %s...\n", model)
	cmd := exec.CommandContext(ctx, "docker", "model", "pull", model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull model %s: %w", model, err)
	}

	slog.Info("Model pulled successfully", "model", model)
	fmt.Printf("Model %s pulled successfully.\n", model)

	return nil
}

func modelExists(ctx context.Context, model string) bool {
	cmd := exec.CommandContext(ctx, "docker", "model", "inspect", model)
	var stderr bytes.Buffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		slog.Debug("Model does not exist", "model", model, "error", strings.TrimSpace(stderr.String()))
		return false
	}
	return true
}

// configureRequest mirrors the model-runner's scheduling.ConfigureRequest structure.
// It specifies per-model runtime configuration options sent via POST /engines/_configure.
type configureRequest struct {
	Model        string                      `json:"model"`
	ContextSize  *int32                      `json:"context-size,omitempty"`
	RuntimeFlags []string                    `json:"runtime-flags,omitempty"`
	Speculative  *speculativeDecodingRequest `json:"speculative,omitempty"`
}

// speculativeDecodingRequest mirrors model-runner's inference.SpeculativeDecodingConfig.
type speculativeDecodingRequest struct {
	DraftModel        string  `json:"draft_model,omitempty"`
	NumTokens         int     `json:"num_tokens,omitempty"`
	MinAcceptanceRate float64 `json:"min_acceptance_rate,omitempty"`
}

// configureModel sends model configuration to Model Runner via POST /engines/_configure.
// This replaces the previous approach of shelling out to `docker model configure`.
func configureModel(ctx context.Context, httpClient *http.Client, baseURL, model string, contextSize *int64, runtimeFlags []string, specOpts *speculativeDecodingOpts) error {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	configureURL := buildConfigureURL(baseURL)
	reqBody := buildConfigureRequest(model, contextSize, runtimeFlags, specOpts)

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal configure request: %w", err)
	}

	// Use a timeout context to avoid blocking client creation indefinitely
	ctx, cancel := context.WithTimeout(ctx, configureTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, configureURL, bytes.NewReader(reqData))
	if err != nil {
		return fmt.Errorf("failed to create configure request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	slog.Debug("Sending model configure request via API",
		"model", model,
		"url", configureURL,
		"context_size", contextSize,
		"runtime_flags", runtimeFlags,
		"speculative_opts", specOpts)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("configure request failed: %w", err)
	}
	defer resp.Body.Close()

	// Model Runner returns 202 Accepted on success
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("configure request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	slog.Debug("Model configure via API completed", "model", model)
	return nil
}

// buildConfigureURL derives the /engines/_configure endpoint URL from the OpenAI base URL.
// It handles various URL formats:
//   - http://host:port/engines/v1/ → http://host:port/engines/_configure
//   - http://_/exp/vDD4.40/engines/v1 → http://_/exp/vDD4.40/engines/_configure
//   - http://host:port/engines/llama.cpp/v1/ → http://host:port/engines/llama.cpp/_configure
func buildConfigureURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		// Fallback: just strip /v1/ suffix and append /_configure
		baseURL = strings.TrimSuffix(baseURL, "/")
		baseURL = strings.TrimSuffix(baseURL, "/v1")
		return baseURL + "/_configure"
	}

	path := u.Path

	// Remove trailing slash for consistent handling
	path = strings.TrimSuffix(path, "/")

	// Remove /v1 suffix to get to the engines path
	path = strings.TrimSuffix(path, "/v1")

	// Append /_configure
	path += "/_configure"

	u.Path = path
	return u.String()
}

// buildConfigureRequest constructs the JSON request body for POST /engines/_configure.
func buildConfigureRequest(model string, contextSize *int64, runtimeFlags []string, specOpts *speculativeDecodingOpts) configureRequest {
	req := configureRequest{
		Model:        model,
		RuntimeFlags: runtimeFlags,
	}

	// Convert int64 context size to int32 as expected by model-runner
	if contextSize != nil {
		cs := int32(*contextSize)
		req.ContextSize = &cs
	}

	if specOpts != nil {
		req.Speculative = &speculativeDecodingRequest{
			DraftModel:        specOpts.draftModel,
			NumTokens:         specOpts.numTokens,
			MinAcceptanceRate: specOpts.acceptanceRate,
		}
	}

	return req
}

func getDockerModelEndpointAndEngine(ctx context.Context) (endpoint, engine string, err error) {
	cmd := exec.CommandContext(ctx, "docker", "model", "status", "--json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", "", errors.New(strings.TrimSpace(stderr.String()))
	}

	type status struct {
		Running  bool              `json:"running"`
		Backends map[string]string `json:"backends"`
		Endpoint string            `json:"endpoint"`
		Engine   string            `json:"engine"`
	}
	var st status
	if err := json.Unmarshal(stdout.Bytes(), &st); err != nil {
		return "", "", err
	}

	endpoint = strings.TrimSpace(st.Endpoint)

	engine = strings.TrimSpace(st.Engine)
	if engine == "" {
		if st.Backends != nil {
			if _, ok := st.Backends["llama.cpp"]; ok {
				engine = "llama.cpp"
			} else {
				for k := range st.Backends {
					engine = k
					break
				}
			}
		}
	}
	engine = cmp.Or(engine, "llama.cpp")

	return endpoint, engine, nil
}

// buildRuntimeFlagsFromModelConfig converts standard ModelConfig fields into backend-specific
// runtime flags that the model-runner understands when launching the engine.
// Currently supports the default engine "llama.cpp". Unknown/unsupported fields are ignored.
func buildRuntimeFlagsFromModelConfig(engine string, cfg *latest.ModelConfig) []string {
	var flags []string
	if cfg == nil {
		return flags
	}
	eng := cmp.Or(strings.TrimSpace(engine), "llama.cpp")
	switch eng {
	// runtime flags mapping for more engines can be added here as needed
	case "llama.cpp":
		if cfg.Temperature != nil {
			flags = append(flags, "--temp", strconv.FormatFloat(*cfg.Temperature, 'f', -1, 64))
		}
		if cfg.TopP != nil {
			flags = append(flags, "--top-p", strconv.FormatFloat(*cfg.TopP, 'f', -1, 64))
		}
		if cfg.FrequencyPenalty != nil {
			flags = append(flags, "--frequency-penalty", strconv.FormatFloat(*cfg.FrequencyPenalty, 'f', -1, 64))
		}
		if cfg.PresencePenalty != nil {
			flags = append(flags, "--presence-penalty", strconv.FormatFloat(*cfg.PresencePenalty, 'f', -1, 64))
		}
		// Note: Context size already handled via context-size field in the configure API request
	default:
		// Unknown engine: no flags
	}
	return flags
}

// jsonSchema is a helper type that implements json.Marshaler for map[string]any
// This allows us to pass schema maps to the OpenAI library which expects json.Marshaler
type jsonSchema map[string]any

func (j jsonSchema) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any(j))
}

// sigmoid applies the sigmoid function to normalize a raw logit score to [0, 1]
// Formula: 1 / (1 + exp(-x))
// This preserves absolute magnitude: positive scores → >0.5, negative scores → <0.5
func sigmoid(x float64) float64 {
	return 1.0 / (1.0 + math.Exp(-x))
}
