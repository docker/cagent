package oci

type ImageConfig struct {
	Config Config `json:"config"`
}

type Config struct {
	Labels     map[string]string `json:"Labels"`
	Env        []string          `json:"Env"`
	Entrypoint []string          `json:"Entrypoint"`
	Cmd        []string          `json:"Cmd"`
	WorkingDir string            `json:"WorkingDir"`
	User       string            `json:"User"`
}
