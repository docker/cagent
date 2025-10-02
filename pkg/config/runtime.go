package config

type RuntimeConfig struct {
	EnvFiles       []string
	ModelsGateway  string
	RedirectURI    string
	GlobalCodeMode bool
	WorkingDir     string
}
