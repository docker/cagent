package config

type RuntimeConfig struct {
	EnvFiles       []string
	ModelsGateway  string
	ToolsGateway   string
	RetryOnFailure bool
}
