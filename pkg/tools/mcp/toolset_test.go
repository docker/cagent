package mcp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/cagent/pkg/model/provider/anthropic"
	"github.com/docker/cagent/pkg/model/provider/dmr"
	"github.com/docker/cagent/pkg/model/provider/gemini"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/tools"
)

const schemaJSON = `
{
    "type": "object",
    "properties": {
      "direction": {
        "description": "Order",
        "enum": [
          "ASC",
          "DESC"
        ],
        "type": "string"
      },
      "labels": {
        "description": "Filter",
        "items": {
          "type": "string"
        },
        "type": "array"
      },
      "perPage": {
        "description": "Results",
        "maximum": 100,
        "minimum": 1,
        "type": "number"
      },
      "repo": {
        "description": "Repository",
        "type": "string"
      }
    },
    "required": ["repo"]
}`

func parseFunctionParameters(t *testing.T, schemaJSON string) tools.FunctionParameters {
	t.Helper()

	var parameters tools.FunctionParameters
	err := json.Unmarshal([]byte(schemaJSON), &parameters)
	require.NoError(t, err)

	return parameters
}

func TestEmptySchemaForGemini(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")

	schema, err := gemini.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object"}`, string(schemaJSON))
}

func TestSchemaForGemini(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := gemini.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `
{
    "type": "object",
    "properties": {
      "direction": {
        "description": "Order",
        "enum": [
          "ASC",
          "DESC"
        ],
        "type": "string"
      },
      "labels": {
        "description": "Filter",
        "items": {
          "type": "string"
        },
        "type": "array"
      },
      "perPage": {
        "description": "Results",
        "maximum": 100,
        "minimum": 1,
        "type": "number"
      },
      "repo": {
        "description": "Repository",
        "type": "string"
      }
    },
    "required": ["repo"]
}`, string(schemaJSON))
}

func TestEmptySchemaForAnthropic(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")
	shema, err := anthropic.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(shema)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schemaJSON))
}

func TestSchemaForAnthropic(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)
	shema, err := anthropic.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(shema)
	require.NoError(t, err)
	assert.JSONEq(t, `
{
	"type": "object",
	"properties": {
		"direction": {
			"description": "Order",
			"enum": ["ASC", "DESC"],
			"type": "string"
		},
		"labels": {
			"description": "Filter",
			"items": {
				"type": "string"
			},
			"type": "array"
		},
		"perPage": {
			"description": "Results",
			"maximum": 100,
			"minimum": 1,
			"type": "number"
		},
		"repo": {
			"description": "Repository",
			"type": "string"
		}
	},
	"required": ["repo"]
}`, string(schemaJSON))
}

func TestEmptySchemaForOpenai(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")

	schema, err := openai.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schemaJSON))
}

func TestSchemaForOpenai(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := openai.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `
{
	"type": "object",
	"properties": {
		"direction": {
			"description": "Order",
			"enum": ["ASC", "DESC"],
			"type": "string"
		},
		"labels": {
			"description": "Filter",
			"items": {
				"type": "string"
			},
			"type": "array"
		},
		"perPage": {
			"description": "Results",
			"maximum": 100,
			"minimum": 1,
			"type": "number"
		},
		"repo": {
			"description": "Repository",
			"type": "string"
		}
	},
	"required": ["repo"]
}`, string(schemaJSON))
}

func TestEmptySchemaForDMR(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")

	schema, err := dmr.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schemaJSON))
}

func TestSchemaForDMR(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := dmr.ConvertParametersToSchema(parameters)
	require.NoError(t, err)

	schemaJSON, err := json.Marshal(schema)
	require.NoError(t, err)
	assert.JSONEq(t, `
{
	"type": "object",
	"properties": {
		"direction": {
			"description": "Order",
			"enum": ["ASC", "DESC"],
			"type": "string"
		},
		"labels": {
			"description": "Filter",
			"items": {
				"type": "string"
			},
			"type": "array"
		},
		"perPage": {
			"description": "Results",
			"maximum": 100,
			"minimum": 1,
			"type": "number"
		},
		"repo": {
			"description": "Repository",
			"type": "string"
		}
	},
	"required": ["repo"]
}`, string(schemaJSON))
}
