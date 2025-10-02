package v2

import (
	"errors"
	"strings"
)

func (t *Config) UnmarshalYAML(unmarshal func(any) error) error {
	type alias Config
	var tmp alias
	if err := unmarshal(&tmp); err != nil {
		return err
	}
	*t = Config(tmp)
	return t.validate()
}

func (t *Config) validate() error {
	for i := range t.Agents {
		agent := t.Agents[i]
		for j := range agent.Toolsets {
			if err := agent.Toolsets[j].validate(); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *Toolset) validate() error {
	// Attributes used on the wrong toolset type.
	if len(t.Shell) > 0 && t.Type != "script" {
		return errors.New("shell can only be used with type 'script'")
	}
	if t.Path != "" && t.Type != "memory" {
		return errors.New("path can only be used with type 'memory'")
	}
	if len(t.PostEdit) > 0 && t.Type != "filesystem" {
		return errors.New("post_edit can only be used with type 'filesystem'")
	}
	if len(t.Env) > 0 && (t.Type != "shell" && t.Type != "script" && t.Type != "mcp") {
		return errors.New("env can only be used with type 'shell', 'script' or 'mcp'")
	}
	if t.Shared && t.Type != "todo" {
		return errors.New("shared can only be used with type 'todo'")
	}
	if t.Command != "" && t.Type != "mcp" {
		return errors.New("command can only be used with type 'mcp'")
	}
	if len(t.Args) > 0 && t.Type != "mcp" {
		return errors.New("args can only be used with type 'mcp'")
	}
	if t.Ref != "" && t.Type != "mcp" {
		return errors.New("ref can only be used with type 'mcp'")
	}
	if (t.Remote.URL != "" || t.Remote.TransportType != "" || len(t.Remote.Headers) > 0) && t.Type != "mcp" {
		return errors.New("remote can only be used with type 'mcp'")
	}
	if t.Config != nil && t.Type != "mcp" {
		return errors.New("config can only be used with type 'mcp'")
	}

	switch t.Type {
	case "memory":
		if t.Path == "" {
			return errors.New("memory toolset requires a path to be set")
		}
	case "mcp":
		count := 0
		if t.Command != "" {
			count++
		}
		if t.Remote.URL != "" {
			count++
		}
		if t.Ref != "" {
			count++
		}
		if count == 0 {
			return errors.New("either command, remote or ref must be set")
		}
		if count > 1 {
			return errors.New("either command, remote or ref must be set, but only one of those")
		}

		if t.Ref != "" && !strings.Contains(t.Ref, "docker:") {
			return errors.New("only docker refs are supported for MCP tools, e.g., 'docker:context7'")
		}
	}

	return nil
}
