# Adding a new provider to Cagent

## Add provider alias

Add a new `Alias` to `ProviderAliases` [`pkg/model/provider/provider.go`](https://github.com/docker/cagent/blob/main/pkg/model/provider/provider.go)

```go
var ProviderAliases = map[string]Alias{
 "requesty": {
  ApiType:     "openai",
  BaseURL:     "https://router.requesty.ai/v1",
  TokenEnvVar: "REQUESTY_API_KEY",
 },
 "azure": {
  ApiType:     "openai",
  TokenEnvVar: "AZURE_API_KEY",
 },
 "YOUR_PROVIDER": {
    ApiType: "openai"
    TokenEnvVar: "YOUR_PROVIDER_API_KEY"
    BaseURL: "https://your-provider.ai/v1"
 }
}
```

## Add custom config if needed (optional)

If your provider requires custom config, like Azure's `api_version`

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
