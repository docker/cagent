package v2

import (
	"github.com/docker/cagent/pkg/config/types"
)

// Config represents the entire configuration file
type Config struct {
	Version  string                 `json:"version,omitempty"`
	Agents   map[string]AgentConfig `json:"agents,omitempty"`
	Models   map[string]ModelConfig `json:"models,omitempty"`
	Metadata Metadata               `json:"metadata,omitempty"`
}

// AgentConfig represents a single agent configuration
type AgentConfig struct {
	Model              string            `json:"model,omitempty"`
	Description        string            `json:"description,omitempty"`
	Toolsets           []Toolset         `json:"toolsets,omitempty"`
	Instruction        string            `json:"instruction,omitempty"`
	SubAgents          []string          `json:"sub_agents,omitempty"`
	AddDate            bool              `json:"add_date,omitempty"`
	AddEnvironmentInfo bool              `json:"add_environment_info,omitempty"`
	CodeModeTools      bool              `json:"code_mode_tools,omitempty"`
	MaxIterations      int               `json:"max_iterations,omitempty"`
	NumHistoryItems    int               `json:"num_history_items,omitempty"`
	AddPromptFiles     []string          `json:"add_prompt_files,omitempty" yaml:"add_prompt_files,omitempty"`
	Commands           types.Commands    `json:"commands,omitempty"`
	StructuredOutput   *StructuredOutput `json:"structured_output,omitempty"`
}

// ModelConfig represents the configuration for a model
type ModelConfig struct {
	Provider          string  `json:"provider,omitempty"`
	Model             string  `json:"model,omitempty"`
	Temperature       float64 `json:"temperature,omitempty"`
	MaxTokens         int     `json:"max_tokens,omitempty"`
	TopP              float64 `json:"top_p,omitempty"`
	FrequencyPenalty  float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty   float64 `json:"presence_penalty,omitempty"`
	BaseURL           string  `json:"base_url,omitempty"`
	ParallelToolCalls *bool   `json:"parallel_tool_calls,omitempty"`
	TokenKey          string  `json:"token_key,omitempty"`
	// ProviderOpts allows provider-specific options. Currently used for "dmr" provider only.
	ProviderOpts map[string]any `json:"provider_opts,omitempty"`
	TrackUsage   *bool          `json:"track_usage,omitempty"`
	// ThinkingBudget controls reasoning effort/budget:
	// - For OpenAI: accepts string levels "minimal", "low", "medium", "high"
	// - For Anthropic: accepts integer token budget (1024-32000)
	// - For other providers: may be ignored
	ThinkingBudget *ThinkingBudget `json:"thinking_budget,omitempty"`
}

type Metadata struct {
	Author  string `json:"author,omitempty"`
	License string `json:"license,omitempty"`
	Readme  string `json:"readme,omitempty"`
}

// Commands represents a set of named prompts for quick-starting conversations.
// It supports two YAML formats:
//
// commands:
//
//	df: "check disk space"
//	ls: "list files"
//
// or
//
// commands:
//   - df: "check disk space"
//   - ls: "list files"
// Commands YAML unmarshalling is implemented in pkg/config/types/commands.go

// ScriptShellToolConfig represents a custom shell tool configuration
type ScriptShellToolConfig struct {
	Cmd         string `json:"cmd"`
	Description string `json:"description"`

	// Args is directly passed as "properties" in the JSON schema
	Args map[string]any `json:"args,omitempty"`

	// Required is directly passed as "required" in the JSON schema
	Required []string `json:"required"`

	Env        map[string]string `json:"env,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
}

// PostEditConfig represents a post-edit command configuration
type PostEditConfig struct {
	Path string `json:"path"`
	Cmd  string `json:"cmd"`
}

// Toolset represents a tool configuration
type Toolset struct {
	Type        string   `json:"type,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Instruction string   `json:"instruction,omitempty"`
	Toon        string   `json:"toon,omitempty"`

	// For the `mcp` tool
	Command string   `json:"command,omitempty"`
	Args    []string `json:"args,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	Remote  Remote   `json:"remote,omitempty"`
	Config  any      `json:"config,omitempty"`

	// For `shell`, `script` or `mcp` tools
	Env map[string]string `json:"env,omitempty"`

	// For the `todo` tool
	Shared bool `json:"shared,omitempty"`

	// For the `memory` tool
	Path string `json:"path,omitempty"`

	// For the `script` tool
	Shell map[string]ScriptShellToolConfig `json:"shell,omitempty"`

	// For the `filesystem` tool - post-edit commands
	PostEdit []PostEditConfig `json:"post_edit,omitempty"`

	// For the `fetch` tool
	Timeout int `json:"timeout,omitempty"`
}

func (t *Toolset) UnmarshalYAML(unmarshal func(any) error) error {
	type alias Toolset
	var tmp alias
	if err := unmarshal(&tmp); err != nil {
		return err
	}
	*t = Toolset(tmp)
	return t.validate()
}

type Remote struct {
	URL           string            `json:"url"`
	TransportType string            `json:"transport_type,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
}

// ThinkingBudget represents reasoning budget configuration.
// It accepts either a string effort level or an integer token budget:
// - String: "minimal", "low", "medium", "high" (for OpenAI)
// - Integer: token count (for Anthropic, range 1024-32768)
type ThinkingBudget struct {
	// Effort stores string-based reasoning effort levels
	Effort string `json:"effort,omitempty"`
	// Tokens stores integer-based token budgets
	Tokens int `json:"tokens,omitempty"`
}

func (t *ThinkingBudget) UnmarshalYAML(unmarshal func(any) error) error {
	// Try integer tokens first
	var n int
	if err := unmarshal(&n); err == nil {
		*t = ThinkingBudget{Tokens: n}
		return nil
	}

	// Try string level
	var s string
	if err := unmarshal(&s); err == nil {
		*t = ThinkingBudget{Effort: s}
		return nil
	}

	return nil
}

// StructuredOutput defines a JSON schema for structured output
type StructuredOutput struct {
	// Name is the name of the response format
	Name string `json:"name"`
	// Description is optional description of the response format
	Description string `json:"description,omitempty"`
	// Schema is a JSON schema object defining the structure
	Schema map[string]any `json:"schema"`
	// Strict enables strict schema adherence (OpenAI only)
	Strict bool `json:"strict,omitempty"`
}
