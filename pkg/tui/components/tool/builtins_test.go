package tool

import (
	"testing"

	"github.com/docker/cagent/pkg/tools"
	"github.com/stretchr/testify/assert"
)

func TestRender_search_files(t *testing.T) {
	tests := []struct {
		name     string
		toolCall tools.ToolCall
		expected string
	}{
		{
			name: "pattern only",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.go"}`,
				},
			},
			expected: "*.go",
		},
		{
			name: "empty pattern",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": ""}`,
				},
			},
			expected: "",
		},
		{
			name: "pattern with valid path",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.go", "path": "/src/app"}`,
				},
			},
			expected: "*.go in /src/app",
		},
		{
			name: "pattern with current directory path",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.go", "path": "."}`,
				},
			},
			expected: "*.go",
		},
		{
			name: "pattern with empty path",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.go", "path": ""}`,
				},
			},
			expected: "*.go",
		},
		{
			name: "pattern with relative path",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "test_*.py", "path": "tests/unit"}`,
				},
			},
			expected: "test_*.py in tests/unit",
		},
		{
			name: "pattern with single exclude",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.go", "excludePatterns": ["*_test.go"]}`,
				},
			},
			expected: "*.go excluding [*_test.go]",
		},
		{
			name: "pattern with multiple excludes",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.js", "excludePatterns": ["node_modules", "*.min.js", "dist"]}`,
				},
			},
			expected: "*.js excluding [node_modules, *.min.js, dist]",
		},
		{
			name: "pattern with empty exclude patterns array",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.py", "excludePatterns": []}`,
				},
			},
			expected: "*.py",
		},
		{
			name: "all fields present",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.java", "path": "src/main/java", "excludePatterns": ["*Test.java", "*Mock.java"]}`,
				},
			},
			expected: "*.java in src/main/java excluding [*Test.java, *Mock.java]",
		},
		{
			name: "all fields with current directory",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "README*", "path": ".", "excludePatterns": ["*.backup"]}`,
				},
			},
			expected: "README* excluding [*.backup]",
		},
		{
			name: "all fields with empty path",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "config.*", "path": "", "excludePatterns": ["*.bak", "*.tmp"]}`,
				},
			},
			expected: "config.* excluding [*.bak, *.tmp]",
		},
		{
			name: "pattern with spaces",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "test file.txt"}`,
				},
			},
			expected: "test file.txt",
		},
		{
			name: "pattern with special regex characters",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "file[0-9]+\\.txt"}`,
				},
			},
			expected: "file[0-9]+\\.txt",
		},
		{
			name: "path with spaces and special characters",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.log", "path": "/var/log/my app/data"}`,
				},
			},
			expected: "*.log in /var/log/my app/data",
		},
		{
			name: "exclude patterns with special characters",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*", "excludePatterns": ["*.~", "#*#", ".#*"]}`,
				},
			},
			expected: "* excluding [*.~, #*#, .#*]",
		},
		{
			name: "invalid JSON",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{invalid json}`,
				},
			},
			expected: "",
		},
		{
			name: "empty arguments",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: ``,
				},
			},
			expected: "",
		},
		{
			name: "search for documentation files",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "*.md", "path": "docs", "excludePatterns": ["node_modules", ".git"]}`,
				},
			},
			expected: "*.md in docs excluding [node_modules, .git]",
		},
		{
			name: "search in deep directory structure",
			toolCall: tools.ToolCall{
				Function: tools.FunctionCall{
					Arguments: `{"pattern": "component_*.tsx", "path": "src/components/ui/buttons/primary", "excludePatterns": ["*.stories.tsx", "*.test.tsx", "*.spec.tsx"]}`,
				},
			},
			expected: "component_*.tsx in src/components/ui/buttons/primary excluding [*.stories.tsx, *.test.tsx, *.spec.tsx]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := render_search_files(tt.toolCall)

			assert.Equal(t, tt.expected, result)
		})
	}
}
