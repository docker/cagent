package latest

import (
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/config/types"
)

func TestCommandsUnmarshal_Map(t *testing.T) {
	var c types.Commands
	input := []byte(`
df: "check disk"
ls: "list files"
`)
	err := yaml.Unmarshal(input, &c)
	require.NoError(t, err)
	require.Equal(t, "check disk", c["df"].Instruction)
	require.Equal(t, "list files", c["ls"].Instruction)
}

func TestCommandsUnmarshal_List(t *testing.T) {
	var c types.Commands
	input := []byte(`
- df: "check disk"
- ls: "list files"
`)
	err := yaml.Unmarshal(input, &c)
	require.NoError(t, err)
	require.Equal(t, "check disk", c["df"].Instruction)
	require.Equal(t, "list files", c["ls"].Instruction)
}

func TestThinkingBudget_MarshalUnmarshal_String(t *testing.T) {
	t.Parallel()

	// Test string effort level
	input := []byte(`thinking_budget: minimal`)
	var config struct {
		ThinkingBudget *ThinkingBudget `yaml:"thinking_budget"`
	}

	// Unmarshal
	err := yaml.Unmarshal(input, &config)
	require.NoError(t, err)
	require.NotNil(t, config.ThinkingBudget)
	require.Equal(t, "minimal", config.ThinkingBudget.Effort)
	require.Equal(t, 0, config.ThinkingBudget.Tokens)

	// Marshal back
	output, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.Equal(t, "thinking_budget: minimal\n", string(output))
}

func TestThinkingBudget_MarshalUnmarshal_Integer(t *testing.T) {
	t.Parallel()

	// Test integer token budget
	input := []byte(`thinking_budget: 8192`)
	var config struct {
		ThinkingBudget *ThinkingBudget `yaml:"thinking_budget"`
	}

	// Unmarshal
	err := yaml.Unmarshal(input, &config)
	require.NoError(t, err)
	require.NotNil(t, config.ThinkingBudget)
	require.Empty(t, config.ThinkingBudget.Effort)
	require.Equal(t, 8192, config.ThinkingBudget.Tokens)

	// Marshal back
	output, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.Equal(t, "thinking_budget: 8192\n", string(output))
}

func TestThinkingBudget_MarshalUnmarshal_NegativeInteger(t *testing.T) {
	t.Parallel()

	// Test negative integer token budget (e.g., -1 for Gemini dynamic thinking)
	input := []byte(`thinking_budget: -1`)
	var config struct {
		ThinkingBudget *ThinkingBudget `yaml:"thinking_budget"`
	}

	// Unmarshal
	err := yaml.Unmarshal(input, &config)
	require.NoError(t, err)
	require.NotNil(t, config.ThinkingBudget)
	require.Empty(t, config.ThinkingBudget.Effort)
	require.Equal(t, -1, config.ThinkingBudget.Tokens)

	// Marshal back
	output, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.Equal(t, "thinking_budget: -1\n", string(output))
}

func TestThinkingBudget_MarshalUnmarshal_Zero(t *testing.T) {
	t.Parallel()

	// Test zero token budget (e.g., 0 for Gemini no thinking)
	input := []byte(`thinking_budget: 0`)
	var config struct {
		ThinkingBudget *ThinkingBudget `yaml:"thinking_budget"`
	}

	// Unmarshal
	err := yaml.Unmarshal(input, &config)
	require.NoError(t, err)
	require.NotNil(t, config.ThinkingBudget)
	require.Empty(t, config.ThinkingBudget.Effort)
	require.Equal(t, 0, config.ThinkingBudget.Tokens)

	// Marshal back
	output, err := yaml.Marshal(config)
	require.NoError(t, err)
	require.Equal(t, "thinking_budget: 0\n", string(output))
}

func TestAgents_UnmarshalYAML_RejectsUnknownFields(t *testing.T) {
	t.Parallel()

	// "instructions" (plural) is not a valid field; the correct field is "instruction" (singular).
	// Agents.UnmarshalYAML must reject it so that typos don't silently drop config.
	input := []byte(`version: "5"
agents:
  root:
    model: openai/gpt-4o
    instructions: "You are a helpful assistant."
`)

	_, err := parse(input)
	require.Error(t, err)
	require.Contains(t, err.Error(), "instructions")
}

func TestAgents_UnmarshalYAML_AcceptsValidConfig(t *testing.T) {
	t.Parallel()

	input := []byte(`version: "5"
agents:
  root:
    model: openai/gpt-4o
    instruction: "You are a helpful assistant."
`)

	cfg, err := parse(input)
	require.NoError(t, err)
	require.Len(t, cfg.Agents, 1)
	require.Equal(t, "root", cfg.Agents[0].Name)
	require.Equal(t, "You are a helpful assistant.", cfg.Agents[0].Instruction)
}

func TestRAGStrategyConfig_MarshalUnmarshal_FlattenedParams(t *testing.T) {
	t.Parallel()

	// Test that params are flattened during unmarshal and remain flattened after marshal
	input := []byte(`type: chunked-embeddings
model: embeddinggemma
database: ./rag/test.db
threshold: 0.5
vector_dimensions: 768
`)

	var strategy RAGStrategyConfig

	// Unmarshal
	err := yaml.Unmarshal(input, &strategy)
	require.NoError(t, err)
	require.Equal(t, "chunked-embeddings", strategy.Type)
	require.Equal(t, "./rag/test.db", mustGetDBString(t, strategy.Database))
	require.NotNil(t, strategy.Params)
	require.Equal(t, "embeddinggemma", strategy.Params["model"])
	require.InEpsilon(t, 0.5, strategy.Params["threshold"], 0.001)
	// YAML may unmarshal numbers as different numeric types (int, uint64, float64)
	require.InEpsilon(t, float64(768), toFloat64(strategy.Params["vector_dimensions"]), 0.001)

	// Marshal back
	output, err := yaml.Marshal(strategy)
	require.NoError(t, err)

	// Verify it's still flattened (no "params:" key)
	outputStr := string(output)
	require.Contains(t, outputStr, "type: chunked-embeddings")
	require.Contains(t, outputStr, "model: embeddinggemma")
	require.Contains(t, outputStr, "threshold: 0.5")
	require.Contains(t, outputStr, "vector_dimensions: 768")
	require.NotContains(t, outputStr, "params:")

	// Unmarshal again to verify round-trip
	var strategy2 RAGStrategyConfig
	err = yaml.Unmarshal(output, &strategy2)
	require.NoError(t, err)
	require.Equal(t, strategy.Type, strategy2.Type)
	require.Equal(t, strategy.Params["model"], strategy2.Params["model"])
	require.Equal(t, strategy.Params["threshold"], strategy2.Params["threshold"])
	// YAML may unmarshal numbers as different numeric types (int, uint64, float64)
	// Just verify the numeric value is correct
	require.InEpsilon(t, float64(768), toFloat64(strategy2.Params["vector_dimensions"]), 0.001)
}

func TestRAGStrategyConfig_MarshalUnmarshal_WithDatabase(t *testing.T) {
	t.Parallel()

	input := []byte(`type: chunked-embeddings
database: ./test.db
model: test-model
`)

	var strategy RAGStrategyConfig
	err := yaml.Unmarshal(input, &strategy)
	require.NoError(t, err)

	// Marshal back
	output, err := yaml.Marshal(strategy)
	require.NoError(t, err)

	// Should contain database as a simple string, not nested with sub-fields
	outputStr := string(output)
	require.Contains(t, outputStr, "database: ./test.db")
	require.NotContains(t, outputStr, "  value:") // Should not be nested with internal fields
	require.Contains(t, outputStr, "model: test-model")
	require.NotContains(t, outputStr, "params:") // Should be flattened
}

func mustGetDBString(t *testing.T, db RAGDatabaseConfig) string {
	t.Helper()
	str, err := db.AsString()
	require.NoError(t, err)
	return str
}

// toFloat64 converts various numeric types to float64 for comparison
func toFloat64(v any) float64 {
	switch val := v.(type) {
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	case float64:
		return val
	case float32:
		return float64(val)
	default:
		return 0
	}
}
