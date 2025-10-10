package codemode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/tools"
)

func TestToolToJsDoc(t *testing.T) {
	tool := tools.Tool{
		Name:        "create_todo",
		Description: "Create new todo\n each of them with a description",
		Parameters: tools.FunctionParameters{
			Type: "object",
			Properties: map[string]any{
				"description": map[string]any{
					"type":        "string",
					"description": "Description of the todo item",
				},
			},
			Required: []string{"description"},
		},
		OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[string]()),
	}

	jsDoc := toolToJsDoc(tool)

	assert.Equal(t, `
/**
 * Create new todo
 * each of them with a description
 * 
 * @param args - Input object containing the parameters.
 * @returns Output - The result of the function execution.
 *
 * Where Input follows the following JSON schema:
 * {
 *   "type": "object",
 *   "properties": {
 *     "description": {
 *       "description": "Description of the todo item",
 *       "type": "string"
 *     }
 *   },
 *   "required": [
 *     "description"
 *   ]
 * }
 *
 * And Output follows the following JSON schema:
 * {
 *   "type": "string"
 * }
 */
function create_todo(args: Input): Output { ... }
`, jsDoc)
}
