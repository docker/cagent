package dmr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sashabaranov/go-openai"
	"golang.org/x/term"

	"github.com/docker/cagent/pkg/chat"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/input"
	"github.com/docker/cagent/pkg/model/provider/base"
	"github.com/docker/cagent/pkg/model/provider/options"
	"github.com/docker/cagent/pkg/tools"
)

// Client represents an DMR client wrapper
// It implements the provider.Provider interface
type Client struct {
	base.Config
	client  *openai.Client
	baseURL string
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

	endpoint, engine, err := getDockerModelEndpointAndEngine(ctx)
	if err != nil {
		slog.Debug("docker model status query failed", "error", err)
	} else {
		// Auto-pull the model if needed
		if err := pullDockerModelIfNeeded(ctx, cfg.Model); err != nil {
			slog.Debug("docker model pull failed", "error", err)
			return nil, err
		}
	}

	clientConfig := openai.DefaultConfig("")

	switch {
	case cfg.BaseURL != "":
		clientConfig.BaseURL = cfg.BaseURL
	case os.Getenv("MODEL_RUNNER_HOST") != "":
		clientConfig.BaseURL = os.Getenv("MODEL_RUNNER_HOST")
	case inContainer():
		// This won't work with Docker CE but we have no way to detect that from inside the container.
		clientConfig.BaseURL = "http://model-runner.docker.internal/engines/v1/"
	case endpoint == "http://model-runner.docker.internal/engines/v1/":
		// Docker Desktop
		clientConfig.BaseURL = "http://_/exp/vDD4.40/engines/v1"
		clientConfig.HTTPClient = &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", "/var/run/docker.sock")
				},
			},
		}
	// NOTE(krissetto): Workaround for a bug in the DMR CLI v0.1.44
	case endpoint == "http://:0/engines/v1/":
		clientConfig.BaseURL = "http://127.0.0.1:12434/engines/v1/"
	default:
		// Docker CE
		clientConfig.BaseURL = endpoint
	}

	// Build runtime flags from ModelConfig and engine
	contextSize, providerRuntimeFlags := parseDMRProviderOpts(cfg)
	configFlags := buildRuntimeFlagsFromModelConfig(engine, cfg)
	finalFlags, warnings := mergeRuntimeFlagsPreferUser(configFlags, providerRuntimeFlags)
	for _, w := range warnings {
		slog.Warn(w)
	}
	slog.Debug("DMR provider_opts parsed", "model", cfg.Model, "context_size", contextSize, "runtime_flags", finalFlags, "engine", engine)
	if err := configureDockerModel(ctx, cfg.Model, contextSize, finalFlags); err != nil {
		slog.Debug("docker model configure skipped or failed", "error", err)
	}

	slog.Debug("DMR client created successfully", "model", cfg.Model, "base_url", clientConfig.BaseURL)

	return &Client{
		Config: base.Config{
			ModelConfig:  cfg,
			ModelOptions: globalOptions,
		},
		client:  openai.NewClientWithConfig(clientConfig),
		baseURL: clientConfig.BaseURL,
	}, nil
}

func inContainer() bool {
	finfo, err := os.Stat("/.dockerenv")
	return err == nil && finfo.Mode().IsRegular()
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
				if idx := strings.Index(tok, "="); idx != -1 {
					k := tok[:idx]
					v := tok[idx+1:]
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

func convertMessages(messages []chat.Message) []openai.ChatCompletionMessage {
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for i := range messages {
		msg := &messages[i]

		// Skip invalid assistant messages upfront. This can happen if the model is out of tokens (max_tokens reached)
		if msg.Role == chat.MessageRoleAssistant && len(msg.ToolCalls) == 0 && len(msg.MultiContent) == 0 && strings.TrimSpace(msg.Content) == "" {
			continue
		}

		openaiMessage := openai.ChatCompletionMessage{
			Role:       string(msg.Role),
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
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

		for _, call := range msg.ToolCalls {
			openaiMessage.ToolCalls = append(openaiMessage.ToolCalls, openai.ToolCall{
				ID:   call.ID,
				Type: openai.ToolType(call.Type),
				Function: openai.FunctionCall{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}

		openaiMessages = append(openaiMessages, openaiMessage)
	}

	var mergedMessages []openai.ChatCompletionMessage

	for i := 0; i < len(openaiMessages); i++ {
		currentMsg := openaiMessages[i]

		if currentMsg.Role == string(chat.MessageRoleSystem) || currentMsg.Role == string(chat.MessageRoleUser) {
			var mergedContent string
			var mergedMultiContent []openai.ChatMessagePart
			j := i

			for j < len(openaiMessages) && openaiMessages[j].Role == currentMsg.Role {
				msgToMerge := openaiMessages[j]

				if len(msgToMerge.MultiContent) == 0 {
					if mergedContent != "" {
						mergedContent += "\n"
					}
					mergedContent += msgToMerge.Content
				} else {
					mergedMultiContent = append(mergedMultiContent, msgToMerge.MultiContent...)
				}
				j++
			}

			mergedMessage := openai.ChatCompletionMessage{
				Role: currentMsg.Role,
			}

			if len(mergedMultiContent) == 0 {
				mergedMessage.Content = mergedContent
			} else {
				mergedMessage.MultiContent = mergedMultiContent
			}

			mergedMessages = append(mergedMessages, mergedMessage)

			i = j - 1
		} else {
			mergedMessages = append(mergedMessages, currentMsg)
		}
	}

	return mergedMessages
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

	// Only set ParallelToolCalls when tools are present; matches OpenAI provider behavior
	if len(requestTools) > 0 && c.ModelConfig.ParallelToolCalls != nil {
		request.ParallelToolCalls = *c.ModelConfig.ParallelToolCalls
	}

	if c.ModelConfig.MaxTokens > 0 {
		request.MaxTokens = c.ModelConfig.MaxTokens
		slog.Debug("DMR request configured with max tokens", "max_tokens", c.ModelConfig.MaxTokens)
	}

	if len(requestTools) > 0 {
		slog.Debug("Adding tools to DMR request", "tool_count", len(requestTools))
		request.Tools = make([]openai.Tool, len(requestTools))
		for i, tool := range requestTools {
			// DMR requires the `description` key to be present; ensure a non-empty value
			// NOTE(krissetto): workaround, remove when fixed upstream, this shouldn't be necceessary
			desc := tool.Description
			if desc == "" {
				desc = "Function " + tool.Name
			}

			parameters, err := ConvertParametersToSchema(tool.Parameters)
			if err != nil {
				slog.Error("Failed to convert tool parameters to DMR schema", "error", err, "tool", tool.Name)
				return nil, fmt.Errorf("failed to convert tool parameters to DMR schema for tool %s: %w", tool.Name, err)
			}

			fd := &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: desc,
				Parameters:  parameters,
			}
			request.Tools[i] = openai.Tool{
				Type:     openai.ToolTypeFunction,
				Function: fd,
			}
			slog.Debug("Added tool to DMR request", "tool_name", tool.Name)
		}
		if c.ModelConfig.ParallelToolCalls != nil {
			request.ParallelToolCalls = *c.ModelConfig.ParallelToolCalls
		}
	}

	// Log the request in JSON format for debugging
	if requestJSON, err := json.Marshal(request); err == nil {
		slog.Debug("DMR chat completion request", "request", string(requestJSON))
	} else {
		slog.Error("Failed to marshal DMR request to JSON", "error", err)
	}
	if c.ModelOptions.StructuredOutput != nil {
		slog.Debug("Adding structured output to DMR request", "structured_output", c.ModelOptions.StructuredOutput)
		request.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:        c.ModelOptions.StructuredOutput.Name,
				Description: c.ModelOptions.StructuredOutput.Description,
				Schema:      jsonSchema(c.ModelOptions.StructuredOutput.Schema),
				Strict:      c.ModelOptions.StructuredOutput.Strict,
			},
		}
	}

	stream, err := c.client.CreateChatCompletionStream(ctx, request)
	if err != nil {
		slog.Error("DMR stream creation failed", "error", err, "model", c.ModelConfig.Model, "base_url", c.baseURL)
		return nil, err
	}

	slog.Debug("DMR chat completion stream created successfully", "model", c.ModelConfig.Model, "base_url", c.baseURL)
	return newStreamAdapter(stream, trackUsage), nil
}

// ConvertParametersToSchema converts parameters to DMR Schema format
func ConvertParametersToSchema(params any) (any, error) {
	return tools.SchemaToMap(params)
}

func parseDMRProviderOpts(cfg *latest.ModelConfig) (contextSize int, runtimeFlags []string) {
	if cfg == nil {
		return 0, nil
	}

	// Context length is now sourced from the standard max_tokens field
	contextSize = cfg.MaxTokens

	if len(cfg.ProviderOpts) > 0 {
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
	}

	return contextSize, runtimeFlags
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

func configureDockerModel(ctx context.Context, model string, contextSize int, runtimeFlags []string) error {
	args := buildDockerModelConfigureArgs(model, contextSize, runtimeFlags)

	cmd := exec.CommandContext(ctx, "docker", args...)
	slog.Debug("Running docker model configure", "model", model, "args", args)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New(strings.TrimSpace(stderr.String()))
	}
	slog.Debug("docker model configure completed", "model", model)
	return nil
}

// buildDockerModelConfigureArgs returns the argument vector passed to `docker` for model configuration.
// It formats context size and runtime flags consistently with the CLI contract.
func buildDockerModelConfigureArgs(model string, contextSize int, runtimeFlags []string) []string {
	args := []string{"model", "configure"}
	if contextSize > 0 {
		args = append(args, "--context-size="+strconv.Itoa(contextSize))
	}
	args = append(args, model)
	if len(runtimeFlags) > 0 {
		args = append(args, "--")
		args = append(args, runtimeFlags...)
	}
	return args
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
	if engine == "" {
		engine = "llama.cpp"
	}

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
	eng := strings.TrimSpace(engine)
	if eng == "" {
		eng = "llama.cpp"
	}
	switch eng {
	// runtime flags mapping for more engines can be added here as needed
	case "llama.cpp":
		if cfg.Temperature > 0 {
			flags = append(flags, "--temp", strconv.FormatFloat(cfg.Temperature, 'f', -1, 64))
		}
		if cfg.TopP > 0 {
			flags = append(flags, "--top-p", strconv.FormatFloat(cfg.TopP, 'f', -1, 64))
		}
		if cfg.FrequencyPenalty > 0 {
			flags = append(flags, "--frequency-penalty", strconv.FormatFloat(cfg.FrequencyPenalty, 'f', -1, 64))
		}
		if cfg.PresencePenalty > 0 {
			flags = append(flags, "--presence-penalty", strconv.FormatFloat(cfg.PresencePenalty, 'f', -1, 64))
		}
		// Note: Context size already handled via --context-size during `docker model configure`
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
