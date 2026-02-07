# Provider Configuration in Cagent

## Custom Providers

The preferred way to add custom providers is through your `agent.yaml` configuration file using the `providers` section. This allows you to define reusable provider configurations without modifying Cagent's source code.

### Example: Custom Provider in agent.yaml

```yaml
providers:
  my_custom_provider:
    api_type: openai_chatcompletions  # or openai_responses
    base_url: https://api.example.com/v1
    token_key: API_KEY_ENV_VAR_NAME

models:
  my_model:
    provider: my_custom_provider
    model: gpt-4o
    max_tokens: 32768

agents:
  root:
    model: my_model
    instruction: You are a helpful assistant.

  # You can also use the shorthand syntax
  subagent:
    model: my_custom_provider/gpt-4o-mini
    instruction: You are a specialized assistant.
```

### Provider Configuration Options

| Field | Description | Default |
|-------|-------------|---------|
| `api_type` | API schema to use (`openai_chatcompletions` or `openai_responses`) | `openai_chatcompletions` |
| `base_url` | Base URL for the provider's API endpoint | - |
| `token_key` | Environment variable name containing the API token | - |

### API Types

- **`openai_chatcompletions`**: Use the OpenAI Chat Completions API schema. This is the default and works with most OpenAI-compatible endpoints.
- **`openai_responses`**: Use the OpenAI Responses API schema. Use this for newer models that require the Responses API format.

### How It Works

When you reference a custom provider in your model configuration:

1. The provider's `base_url` is applied to the model if not already set
2. The provider's `token_key` is applied to the model if not already set
3. The provider's `api_type` is stored in `provider_opts.api_type` (model-level overrides take precedence)
4. The model can then be used with the appropriate API client

---

## Built-in Provider Aliases (For Developers)

If you want to add a new built-in provider alias to Cagent itself, add a new `Alias` to `Aliases` in [`pkg/model/provider/provider.go`](https://github.com/docker/cagent/blob/main/pkg/model/provider/provider.go)

```go
var Aliases = map[string]Alias{
 "requesty": {
  APIType:     "openai",
  BaseURL:     "https://router.requesty.ai/v1",
  TokenEnvVar: "REQUESTY_API_KEY",
 },
 "azure": {
  APIType:     "openai",
  TokenEnvVar: "AZURE_API_KEY",
 },
 "YOUR_PROVIDER": {
    APIType: "openai"
    TokenEnvVar: "YOUR_PROVIDER_API_KEY"
    BaseURL: "https://your-provider.ai/v1"
 }
}
```

## Add custom config if needed (optional)

If your provider requires custom config, like Azure's `api_version` or DMR's speculative decoding options

```yaml
models:
  azure_model:
    provider: azure
    model: gpt-4o
    base_url: https://your-llm.openai.azure.com
    provider_opts:
      api_version: 2024-12-01-preview
  # custom option example
  your_model:
    provider: your_provider
    model: gpt-4o
    provider_opts:
      your_custom_option: your_custom_value
  # DMR with speculative decoding
  dmr_model:
    provider: dmr
    model: ai/qwen3:14B
    provider_opts:
      speculative_draft_model: ai/qwen3:1B
      speculative_num_tokens: 5
      speculative_acceptance_rate: 0.8
```

edit [`pkg/model/provider/openai/client.go`](https://github.com/docker/cagent/blob/main/pkg/model/provider/openai/client.go)

```go
switch cfg.Provider { //nolint:gocritic
   case "azure":
     if apiVersion, exists := cfg.ProviderOpts["api_version"]; exists {
       slog.Debug("Setting API version", "api_version", apiVersion)
       if apiVersionStr, ok := apiVersion.(string); ok {
          openaiConfig.APIVersion = apiVersionStr
       }
      }
   case "your_provider":
     if yourCustomOption, exists := cfg.ProviderOpts["your_custom_option"]; exists {
       slog.Debug("Setting your custom option", "your_custom_option", yourCustomOption)
       if yourCustomOptionStr, ok := yourCustomOption.(string); ok {
        openaiConfig.yourCustomOption = yourCustomOptionStr
       }
      }
   }
```

## DMR Provider Specific Options

The DMR provider supports speculative decoding for faster inference. Configure it using `provider_opts`:

- `speculative_draft_model` (string): Model to use for draft predictions
- `speculative_num_tokens` (int): Number of tokens to generate speculatively
- `speculative_acceptance_rate` (float): Acceptance rate threshold for speculative tokens

All three options are sent to Model Runner via its internal `POST /engines/_configure` API endpoint.

You can also pass any flag of the underlying model runtime (llama.cpp or vllm) using the `runtime_flags` option
