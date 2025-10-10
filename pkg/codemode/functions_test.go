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
		Description: "Create new todo",
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

	assert.Equal(t, `===== create_todo =====

Create new todo

create_todo(args: ArgsObject): string

where type ArgsObject = {
  description: string // Description of the todo item
};
`, jsDoc)
}
