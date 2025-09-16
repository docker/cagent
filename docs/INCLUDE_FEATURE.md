# YAML Include Feature

The `!include` tag allows you to include external YAML files in your cagent configurations, enabling better organization, reusability, and maintainability of your agent configurations.

## Overview

The include feature provides:
- **Modularity**: Split large configurations into focused, manageable files
- **Reusability**: Share common configurations across multiple agents
- **Maintainability**: Update shared configurations in one place
- **Organization**: Keep related configurations together

## Syntax

Use the `!include` tag to include external YAML files:

```yaml
# Include an entire section
models: !include shared-models.yaml

# Include in sequences
toolsets: !include development-toolsets.yaml

# Include nested content
some_config:
  nested: !include nested-config.yaml
```

## How It Works

Include processing happens during configuration loading, before the YAML is parsed into the final configuration structure. The include processor:

1. **Parses the YAML** to identify `!include` tags
2. **Resolves paths** relative to the including file
3. **Loads included files** and processes any nested includes
4. **Replaces include tags** with the included content
5. **Continues parsing** the final merged YAML

## Path Resolution

Include paths are resolved relative to the file containing the `!include` tag:

- `!include models.yaml` - file in the same directory
- `!include ../common/models.yaml` - file in a parent directory  
- `!include configs/toolsets.yaml` - file in a subdirectory
- `!include /absolute/path/file.yaml` - absolute path (not recommended)

## Security Features

The include system includes several security protections:

### Path Validation
- Cleans and normalizes all paths before processing
- Still validates against basic directory traversal sequences when no base directory restriction is needed

### Circular Include Detection
- Tracks included files to prevent infinite loops
- Throws clear error messages when circular includes are detected
- Example:
  ```
  file_a.yaml includes file_b.yaml
  file_b.yaml includes file_a.yaml  // ❌ Circular include detected
  ```

### Sandbox Restrictions
- When using `LoadConfigSecure()`, the main config file must be within the specified allowed directory
- Include files can be loaded from anywhere on the filesystem for maximum flexibility

## Examples

### Basic Model Sharing

**shared-models.yaml:**
```yaml
claude-dev:
  provider: anthropic
  model: claude-sonnet-4-0
  max_tokens: 32000
  temperature: 0.1

gpt-dev:
  provider: openai
  model: gpt-4
  max_tokens: 16000
  temperature: 0.1
```

**agent.yaml:**
```yaml
version: "2"
models: !include shared-models.yaml

agents:
  root:
    model: claude-dev
    description: "Development agent using shared models"
```

### Toolset Reuse

**dev-toolsets.yaml:**
```yaml
- type: shell
- type: filesystem
- type: todo
```

**Multiple agents using the same toolsets:**
```yaml
version: "2"
agents:
  coder:
    model: claude-dev
    description: "Coding specialist"
    toolsets: !include dev-toolsets.yaml
    
  reviewer:
    model: gpt-dev  
    description: "Code reviewer"
    toolsets: !include dev-toolsets.yaml
```

### Nested Includes

**base-config.yaml:**
```yaml
models: !include models/shared-models.yaml
common_toolsets: !include toolsets/development.yaml
```

**agent.yaml:**
```yaml
version: "2"
# Include base configuration
base: !include base-config.yaml

agents:
  root:
    model: claude-dev
    toolsets: !include base.common_toolsets
```

## Best Practices

### 1. Organize by Purpose
Group related configurations together:
```
configs/
├── models/
│   ├── shared-models.yaml
│   └── specialized-models.yaml
├── toolsets/
│   ├── development.yaml
│   ├── web-dev.yaml
│   └── data-science.yaml
└── instructions/
    ├── coding-instructions.yaml
    └── creative-instructions.yaml
```

### 2. Use Descriptive Names
Make the purpose clear from filenames:
- ✅ `shared-models.yaml`
- ✅ `development-toolsets.yaml`
- ✅ `web-automation-tools.yaml`
- ❌ `config1.yaml`
- ❌ `stuff.yaml`

### 3. Keep Includes Focused
Each include file should have a single, clear purpose:
- Models configuration
- Specific toolset
- Instruction templates
- Environment-specific settings

### 4. Document Dependencies
In your main config files, add comments explaining includes:
```yaml
# Include shared development models (Claude, GPT-4)
models: !include shared-models.yaml

# Include standard development toolsets (shell, filesystem, todo)
toolsets: !include development-toolsets.yaml
```

### 5. Version Control Everything
Ensure all included files are tracked in version control alongside the main configurations.

## Error Handling

### Common Errors and Solutions

**File Not Found:**
```
Error: failed to read include file 'missing.yaml': no such file or directory
```
- Check the file path is correct
- Verify the file exists relative to the including file

**Circular Include:**
```
Error: circular include detected: config.yaml
```
- Remove circular dependencies between files
- Restructure includes to avoid loops

**Invalid YAML:**
```
Error: failed to parse include file 'bad.yaml': yaml: line 5: found unexpected end of stream
```
- Fix YAML syntax in the included file
- Validate YAML structure

**File Not Found:**
```
Error: failed to read include file '../../../etc/passwd': no such file or directory
```
- Ensure the file path exists and is accessible
- Check file permissions if the file exists but cannot be read

## Limitations

### No Templating
Includes don't support variable substitution:
```yaml
# ❌ This doesn't work
model_name: claude-dev
models: !include models/${model_name}.yaml
```

### Static Resolution
Includes are processed at config load time, not dynamically during runtime.

### File-Based Only
Only supports including from local files, not from URLs or other sources.

### No Conditional Includes
No support for conditional includes based on environment or other factors:
```yaml
# ❌ This doesn't work
models: !include ${NODE_ENV == 'dev' ? 'dev-models.yaml' : 'prod-models.yaml'}
```

## Implementation Details

The include functionality is implemented in `pkg/config/config.go` with the following key functions:

- `processIncludes()` - Main entry point for processing includes
- `processIncludeNode()` - Recursively processes YAML nodes  
- `processIncludeTag()` - Handles individual include tags
- `ValidatePathInDirectory()` - Security validation for include paths

The processing happens before version-specific parsing, ensuring includes work across all configuration versions (v0, v1, v2).

## Testing

Comprehensive tests are available in `pkg/config/include_test.go` covering:
- Basic include functionality
- Nested includes
- Relative path resolution
- Circular include detection
- Security path validation
- Include within sequences
- End-to-end configuration loading

Run tests with:
```bash
go test ./pkg/config -v -run TestProcessIncludes
go test ./pkg/config -v -run TestLoadConfigWithIncludes
```
