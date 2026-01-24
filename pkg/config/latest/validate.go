package latest

import (
	"errors"
	"fmt"
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
	for agentName, agent := range t.Agents {
		for j := range agent.Toolsets {
			if err := agent.Toolsets[j].validate(); err != nil {
				return err
			}
		}
		if agent.Hooks != nil {
			if err := agent.Hooks.validate(); err != nil {
				return err
			}
		}
		// Validate agent memory references exist in top-level memory map
		for _, memRef := range agent.Memory {
			if _, exists := t.Memory[memRef]; !exists {
				return fmt.Errorf("agent %q: references undefined memory %q", agentName, memRef)
			}
		}
	}

	for name, mem := range t.Memory {
		if err := mem.validate(name); err != nil {
			return err
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
	if t.IgnoreVCS != nil && t.Type != "filesystem" {
		return errors.New("ignore_vcs can only be used with type 'filesystem'")
	}
	if len(t.Env) > 0 && (t.Type != "shell" && t.Type != "script" && t.Type != "mcp" && t.Type != "lsp") {
		return errors.New("env can only be used with type 'shell', 'script', 'mcp' or 'lsp'")
	}
	if t.Sandbox != nil && t.Type != "shell" {
		return errors.New("sandbox can only be used with type 'shell'")
	}
	if t.Shared && t.Type != "todo" {
		return errors.New("shared can only be used with type 'todo'")
	}
	if t.Command != "" && t.Type != "mcp" && t.Type != "lsp" {
		return errors.New("command can only be used with type 'mcp' or 'lsp'")
	}
	if len(t.Args) > 0 && t.Type != "mcp" && t.Type != "lsp" {
		return errors.New("args can only be used with type 'mcp' or 'lsp'")
	}
	if t.Ref != "" && t.Type != "mcp" {
		return errors.New("ref can only be used with type 'mcp'")
	}
	if (t.Remote.URL != "" || t.Remote.TransportType != "") && t.Type != "mcp" {
		return errors.New("remote can only be used with type 'mcp'")
	}
	if (len(t.Remote.Headers) > 0) && (t.Type != "mcp" && t.Type != "a2a") {
		return errors.New("headers can only be used with type 'mcp' or 'a2a'")
	}
	if t.Config != nil && t.Type != "mcp" {
		return errors.New("config can only be used with type 'mcp'")
	}
	if t.URL != "" && t.Type != "a2a" {
		return errors.New("url can only be used with type 'a2a'")
	}
	if t.Name != "" && (t.Type != "mcp" && t.Type != "a2a") {
		return errors.New("name can only be used with type 'mcp' or 'a2a'")
	}

	switch t.Type {
	case "shell":
		if t.Sandbox != nil && len(t.Sandbox.Paths) == 0 {
			return errors.New("sandbox requires at least one path to be set")
		}
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
	case "a2a":
		if t.URL == "" {
			return errors.New("a2a toolset requires a url to be set")
		}
	case "lsp":
		if t.Command == "" {
			return errors.New("lsp toolset requires a command to be set")
		}
	}

	return nil
}

func (m *MemoryConfig) validate(name string) error {
	if m.Kind == "" {
		return fmt.Errorf("memory %q: kind is required", name)
	}

	validKinds := map[string]bool{
		"sqlite":     true,
		"neo4j":      true,
		"qdrant":     true,
		"redis":      true,
		"whiteboard": true,
	}
	if !validKinds[m.Kind] {
		return fmt.Errorf("memory %q: invalid kind %q, must be one of: sqlite, neo4j, qdrant, redis, whiteboard", name, m.Kind)
	}

	// Validate strategy if provided
	if m.Strategy != "" {
		validStrategies := map[string]bool{
			"long_term":  true, // Persistent RAG-style memory (sqlite, neo4j, qdrant)
			"short_term": true, // Ephemeral whiteboard-style memory (whiteboard, redis)
		}
		if !validStrategies[m.Strategy] {
			return fmt.Errorf("memory %q: invalid strategy %q, must be one of: long_term, short_term", name, m.Strategy)
		}

		// Validate strategy matches kind semantics
		longTermKinds := map[string]bool{"sqlite": true, "neo4j": true, "qdrant": true}
		shortTermKinds := map[string]bool{"whiteboard": true, "redis": true}

		if m.Strategy == "long_term" && shortTermKinds[m.Kind] {
			return fmt.Errorf("memory %q: kind %q is not suitable for long_term strategy (use sqlite, neo4j, or qdrant)", name, m.Kind)
		}
		if m.Strategy == "short_term" && longTermKinds[m.Kind] && m.Kind != "redis" {
			// Note: redis can be used for both strategies; sqlite/neo4j/qdrant are long-term only
			if m.Kind != "redis" {
				return fmt.Errorf("memory %q: kind %q is not suitable for short_term strategy (use whiteboard or redis)", name, m.Kind)
			}
		}
	}

	// Validate mode if provided
	if m.Mode != "" {
		validModes := map[string]bool{
			"read_write":  true, // Default: full read/write access
			"read_only":   true, // Read-only access (useful for shared knowledge bases)
			"append_only": true, // Append-only (event-log style, no updates/deletes)
		}
		if !validModes[m.Mode] {
			return fmt.Errorf("memory %q: invalid mode %q, must be one of: read_write, read_only, append_only", name, m.Mode)
		}
	}

	// Validate auth completeness if present (check Connection is not nil first)
	if m.Connection != nil && m.Connection.Auth != nil {
		auth := m.Connection.Auth
		hasUserPass := auth.Username != "" || auth.Password != ""
		hasToken := auth.Token != ""

		if hasUserPass && hasToken {
			return fmt.Errorf("memory %q: auth must use either username/password or token, not both", name)
		}
		if hasUserPass && (auth.Username == "" || auth.Password == "") {
			return fmt.Errorf("memory %q: auth requires both username and password when using user/password auth", name)
		}
	}

	// For sqlite, path is required
	if m.Kind == "sqlite" && m.Path == "" {
		return fmt.Errorf("memory %q: sqlite requires a path", name)
	}

	// For remote backends, connection URL is typically required
	remoteKinds := map[string]bool{"neo4j": true, "qdrant": true, "redis": true}
	if remoteKinds[m.Kind] && (m.Connection == nil || m.Connection.URL == "") {
		return fmt.Errorf("memory %q: %s requires connection.url", name, m.Kind)
	}

	// TTL validation: only meaningful for short-term/ephemeral memory
	if m.TTL > 0 && m.Kind != "whiteboard" && m.Kind != "redis" {
		return fmt.Errorf("memory %q: ttl is only supported for whiteboard and redis kinds", name)
	}

	return nil
}
