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

	var schema map[string]any
	err := json.Unmarshal([]byte(schemaJSON), &schema)
	require.NoError(t, err)

	return inputSchemaToFunctionParameters(schema)
}

func TestEmptySchemaForGemini(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")
	schema, err := json.Marshal(gemini.ConvertParametersToSchema(parameters))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "OBJECT"}`, string(schema))
}

func TestSchemaForGemini(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)
	schema, err := json.Marshal(gemini.ConvertParametersToSchema(parameters))

	require.NoError(t, err)
	assert.JSONEq(t, `
{
	"type": "OBJECT",
	"properties": {
		"direction": {
			"description": "Order",
			"type": "STRING"
		},
		"labels": {
			"description": "Filter",
			"items": {
				"type": "STRING"
			},
			"type": "ARRAY"
		},
		"perPage": {
			"description": "Results",
			"type": "NUMBER"
		},
		"repo": {
			"description": "Repository",
			"type": "STRING"
		}
	},
	"required": ["repo"]
}`, string(schema))
}

func TestEmptySchemaForAnthropic(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")
	schema, err := json.Marshal(anthropic.ConvertParametersToSchema(parameters))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schema))
}

func TestSchemaForAnthropic(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := json.Marshal(anthropic.ConvertParametersToSchema(parameters))
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
}`, string(schema))
}

func TestEmptySchemaForOpenai(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")
	schema, err := json.Marshal(openai.ConvertParametersToSchema(parameters))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schema))
}

func TestSchemaForOpenai(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := json.Marshal(openai.ConvertParametersToSchema(parameters))
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
}`, string(schema))
}

func TestEmptySchemaForDMR(t *testing.T) {
	parameters := parseFunctionParameters(t, "{}")
	schema, err := json.Marshal(dmr.ConvertParametersToSchema(parameters))

	require.NoError(t, err)
	assert.JSONEq(t, `{"type": "object", "properties": {}}`, string(schema))
}

func TestSchemaForDMR(t *testing.T) {
	parameters := parseFunctionParameters(t, schemaJSON)

	schema, err := json.Marshal(dmr.ConvertParametersToSchema(parameters))
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
}`, string(schema))
}
