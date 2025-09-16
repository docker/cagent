package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessIncludes(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_include_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Test 1: Basic include functionality
	t.Run("BasicInclude", func(t *testing.T) {
		// Create an include file
		includeContent := `
models:
  shared-claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 32000
`
		includePath := filepath.Join(tempDir, "shared-models.yaml")
		if err := os.WriteFile(includePath, []byte(includeContent), 0644); err != nil {
			t.Fatalf("Failed to write include file: %v", err)
		}

		// Create main config with include
		mainContent := `version: "2"
models: !include shared-models.yaml
agents:
  root:
    model: shared-claude
    description: Test agent
`
		processed, err := processIncludes([]byte(mainContent), tempDir)
		if err != nil {
			t.Fatalf("Failed to process includes: %v", err)
		}

		// Verify the include was processed
		processedStr := string(processed)
		if !strings.Contains(processedStr, "shared-claude") {
			t.Errorf("Include content not found in processed YAML")
		}
		if !strings.Contains(processedStr, "anthropic") {
			t.Errorf("Include content not properly merged")
		}
		if strings.Contains(processedStr, "!include") {
			t.Errorf("Include tag should be processed and removed")
		}
	})

	// Test 2: Nested includes
	t.Run("NestedIncludes", func(t *testing.T) {
		// Create nested include file
		nestedContent := `
shared_tools:
  - type: shell
  - type: filesystem
`
		nestedPath := filepath.Join(tempDir, "nested-tools.yaml")
		if err := os.WriteFile(nestedPath, []byte(nestedContent), 0644); err != nil {
			t.Fatalf("Failed to write nested include file: %v", err)
		}

		// Create middle include file that includes the nested one
		middleContent := `
models:
  test-model:
    provider: openai
    model: gpt-4
toolsets: !include nested-tools.yaml
`
		middlePath := filepath.Join(tempDir, "middle.yaml")
		if err := os.WriteFile(middlePath, []byte(middleContent), 0644); err != nil {
			t.Fatalf("Failed to write middle include file: %v", err)
		}

		// Create main config
		mainContent := `version: "2"
config: !include middle.yaml
agents:
  root:
    description: Test agent with nested includes
`
		processed, err := processIncludes([]byte(mainContent), tempDir)
		if err != nil {
			t.Fatalf("Failed to process nested includes: %v", err)
		}

		processedStr := string(processed)
		if !strings.Contains(processedStr, "test-model") {
			t.Errorf("Middle include content not found")
		}
		if !strings.Contains(processedStr, "shared_tools") {
			t.Errorf("Nested include content not found")
		}
	})

	// Test 3: Include with relative paths
	t.Run("RelativePaths", func(t *testing.T) {
		// Create subdirectory
		subDir := filepath.Join(tempDir, "configs")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}

		// Create include file in subdirectory
		includeContent := `
test_value: "from_subdir"
`
		includePath := filepath.Join(subDir, "sub-config.yaml")
		if err := os.WriteFile(includePath, []byte(includeContent), 0644); err != nil {
			t.Fatalf("Failed to write include file: %v", err)
		}

		// Create main config that includes with relative path
		mainContent := `version: "2"
included: !include configs/sub-config.yaml
agents:
  root:
    description: Test relative path
`
		processed, err := processIncludes([]byte(mainContent), tempDir)
		if err != nil {
			t.Fatalf("Failed to process relative include: %v", err)
		}

		if !strings.Contains(string(processed), "from_subdir") {
			t.Errorf("Relative include content not found")
		}
	})

	// Test 4: Circular include detection
	t.Run("CircularInclude", func(t *testing.T) {
		// Create file A that includes B
		contentA := `
data_a: value_a
include_b: !include file_b.yaml
`
		pathA := filepath.Join(tempDir, "file_a.yaml")
		if err := os.WriteFile(pathA, []byte(contentA), 0644); err != nil {
			t.Fatalf("Failed to write file A: %v", err)
		}

		// Create file B that includes A (circular)
		contentB := `
data_b: value_b
include_a: !include file_a.yaml
`
		pathB := filepath.Join(tempDir, "file_b.yaml")
		if err := os.WriteFile(pathB, []byte(contentB), 0644); err != nil {
			t.Fatalf("Failed to write file B: %v", err)
		}

		// Process should detect circular include
		_, err := processIncludes([]byte(contentA), tempDir)
		if err == nil {
			t.Errorf("Expected circular include error, got none")
		}
		if !strings.Contains(err.Error(), "circular include") {
			t.Errorf("Expected 'circular include' error, got: %v", err)
		}
	})

	// Test 5: Invalid include path
	t.Run("InvalidIncludePath", func(t *testing.T) {
		mainContent := `version: "2"
invalid: !include nonexistent-file.yaml
`
		_, err := processIncludes([]byte(mainContent), tempDir)
		if err == nil {
			t.Errorf("Expected error for nonexistent include file")
		}
	})

	// Test 6: Empty include path
	t.Run("EmptyIncludePath", func(t *testing.T) {
		mainContent := `version: "2"
empty: !include ""
`
		_, err := processIncludes([]byte(mainContent), tempDir)
		if err == nil {
			t.Errorf("Expected error for empty include path")
		}
		if !strings.Contains(err.Error(), "empty include path") {
			t.Errorf("Expected 'empty include path' error, got: %v", err)
		}
	})

	// Test 7: Directory traversal now allowed (but file doesn't exist)
	t.Run("DirectoryTraversalAllowed", func(t *testing.T) {
		mainContent := `version: "2"
malicious: !include ../../etc/passwd
`
		_, err := processIncludes([]byte(mainContent), tempDir)
		// Should fail because file doesn't exist, not because of path validation
		if err == nil {
			t.Errorf("Expected error for nonexistent file")
		}
		if !strings.Contains(err.Error(), "failed to read include file") {
			t.Errorf("Expected 'failed to read include file' error, got: %v", err)
		}
	})

	// Test 8: Include within sequence
	t.Run("IncludeInSequence", func(t *testing.T) {
		// Create include file with toolset config
		includeContent := `
type: shell
`
		includePath := filepath.Join(tempDir, "shell-toolset.yaml")
		if err := os.WriteFile(includePath, []byte(includeContent), 0644); err != nil {
			t.Fatalf("Failed to write include file: %v", err)
		}

		// Create main config with include in sequence
		mainContent := `version: "2"
agents:
  root:
    description: Test agent
    toolsets:
      - !include shell-toolset.yaml
      - type: filesystem
`
		processed, err := processIncludes([]byte(mainContent), tempDir)
		if err != nil {
			t.Fatalf("Failed to process include in sequence: %v", err)
		}

		processedStr := string(processed)
		if !strings.Contains(processedStr, "type: shell") {
			t.Errorf("Include content not found in sequence")
		}
		if !strings.Contains(processedStr, "type: filesystem") {
			t.Errorf("Other sequence items not preserved")
		}
	})
}

// Test end-to-end config loading with includes
func TestLoadConfigWithIncludes(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "config_load_include_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create shared models file
	modelsContent := `shared-claude:
  provider: anthropic
  model: claude-sonnet-4-0
  max_tokens: 32000
  temperature: 0.7
shared-gpt:
  provider: openai
  model: gpt-4
  max_tokens: 16000`
	modelsPath := filepath.Join(tempDir, "shared-models.yaml")
	if err := os.WriteFile(modelsPath, []byte(modelsContent), 0644); err != nil {
		t.Fatalf("Failed to write models file: %v", err)
	}

	// Create shared toolsets file
	toolsetsContent := `- type: shell
- type: filesystem
- type: todo`
	toolsetsPath := filepath.Join(tempDir, "shared-toolsets.yaml")
	if err := os.WriteFile(toolsetsPath, []byte(toolsetsContent), 0644); err != nil {
		t.Fatalf("Failed to write toolsets file: %v", err)
	}

	// Create main config file
	mainContent := `#!/usr/bin/env cagent run
version: "2"

# Include shared models
models: !include shared-models.yaml

agents:
  root:
    model: shared-claude
    description: "Expert code analysis and development assistant"
    instruction: |
      You are an expert developer with deep knowledge of code analysis.
    # Include shared toolsets
    toolsets: !include shared-toolsets.yaml
`
	mainPath := filepath.Join(tempDir, "main-agent.yaml")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("Failed to write main config file: %v", err)
	}

	// Test loading the config
	config, err := LoadConfigSecure(mainPath, tempDir)
	if err != nil {
		t.Fatalf("Failed to load config with includes: %v", err)
	}

	// Verify the config was loaded correctly
	if len(config.Models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(config.Models))
	}

	if _, exists := config.Models["shared-claude"]; !exists {
		t.Errorf("shared-claude model not found")
	}

	if _, exists := config.Models["shared-gpt"]; !exists {
		t.Errorf("shared-gpt model not found")
	}

	if len(config.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(config.Agents))
	}

	rootAgent, exists := config.Agents["root"]
	if !exists {
		t.Fatalf("root agent not found")
	}

	if rootAgent.Model != "shared-claude" {
		t.Errorf("Expected model 'shared-claude', got '%s'", rootAgent.Model)
	}

	if len(rootAgent.Toolsets) != 3 {
		t.Errorf("Expected 3 toolsets, got %d", len(rootAgent.Toolsets))
	}

	// Check specific toolsets
	expectedTypes := map[string]bool{"shell": false, "filesystem": false, "todo": false}
	for _, toolset := range rootAgent.Toolsets {
		if _, exists := expectedTypes[toolset.Type]; exists {
			expectedTypes[toolset.Type] = true
		}
	}

	for toolsetType, found := range expectedTypes {
		if !found {
			t.Errorf("Expected toolset type '%s' not found", toolsetType)
		}
	}
}
