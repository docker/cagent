package v2

import "gopkg.in/yaml.v3"

func Load(data []byte) (Config, error) {
	var cfg Config
	err := yaml.Unmarshal(data, &cfg)
	return cfg, err
}
