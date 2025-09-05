package v1

import "github.com/stretchr/testify/assert/yaml"

func Load(data []byte) (Config, error) {
	var cfg Config
	err := yaml.Unmarshal(data, &cfg)
	return cfg, err
}
