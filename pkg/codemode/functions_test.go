package codemode

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/docker/cagent/pkg/tools"
)

func TestToolToJsDoc(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
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
		},
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

func TestToolToJsDocArrayOutput(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:        "list_files",
			Description: "List files in directory",
			Parameters: tools.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "Directory path",
					},
				},
				Required: []string{"path"},
			},
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[[]string]()),
		},
	}

	jsDoc := toolToJsDoc(tool)

	assert.Equal(t, `===== list_files =====

List files in directory

list_files(args: ArgsObject): string[]

where type ArgsObject = {
  path: string // Directory path
};
`, jsDoc)
}

func TestToolToJsDocObjectOutput(t *testing.T) {
	type FileInfo struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:        "get_file_info",
			Description: "Get file information",
			Parameters: tools.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "File path",
					},
				},
				Required: []string{"path"},
			},
			OutputSchema: tools.ToOutputSchemaSchemaMust(reflect.TypeFor[FileInfo]()),
		},
	}

	jsDoc := toolToJsDoc(tool)

	assert.Equal(t, `===== get_file_info =====

Get file information

get_file_info(args: ArgsObject): object

where type ArgsObject = {
  path: string // File path
};
`, jsDoc)
}

func TestToolToJsDocNoSchema(t *testing.T) {
	tool := tools.Tool{
		Function: &tools.FunctionDefinition{
			Name:        "legacy_tool",
			Description: "Legacy tool without schema",
			Parameters: tools.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
			OutputSchema: nil,
		},
	}

	jsDoc := toolToJsDoc(tool)

	assert.Equal(t, `===== legacy_tool =====

Legacy tool without schema

legacy_tool(): any
`, jsDoc)
}
