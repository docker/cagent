# YAML Include Examples

This directory demonstrates the `!include` functionality in cagent YAML configurations.

## Overview

The `!include` tag allows you to include external YAML files in your agent configurations, enabling:
- **Reusability**: Share common configurations across multiple agents
- **Modularity**: Organize complex configurations into smaller, focused files
- **Maintainability**: Update shared configurations in one place

## Basic Usage

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

## Path Resolution

Include paths are resolved relative to the including file:
- `!include shared-models.yaml` - file in the same directory
- `!include ../common/models.yaml` - file in a parent directory
- `!include configs/dev-tools.yaml` - file in a subdirectory

Absolute paths are also supported but not recommended for portability.

## Security

Include processing includes several security features:
- **Path validation**: Basic path cleaning and normalization
- **Circular detection**: Prevents infinite include loops
- **Flexible access**: Include files can be accessed from anywhere on the filesystem

## Example Files

### Shared Configurations

- **`shared-models.yaml`**: Common model configurations for different use cases
- **`development-toolsets.yaml`**: Standard development tools (shell, filesystem, todo)
- **`web-toolsets.yaml`**: Web development tools including browser automation
- **`shared-instructions.yaml`**: Reusable instruction templates

### Example Agents

- **`code-agent.yaml`**: Development-focused agent using shared models and dev toolsets
- **`creative-agent.yaml`**: Creative writing agent with shared models
- **`web-dev-agent.yaml`**: Web development specialist with browser automation
- **`multi-model-agent.yaml`**: Advanced agent using multiple models and sub-agents

## Testing the Examples

You can test any of these configurations with:

```bash
# Test the code agent
cagent run examples/includes/code-agent.yaml

# Test the web development agent  
cagent run examples/includes/web-dev-agent.yaml

# Test the multi-model agent
cagent run examples/includes/multi-model-agent.yaml
```

## Best Practices

1. **Organize by purpose**: Group related configurations (models, toolsets, instructions)
2. **Use descriptive names**: Make include file purposes clear from their names
3. **Keep includes focused**: Each include file should have a single, clear purpose
4. **Document dependencies**: Note which files depend on specific includes
5. **Version control**: Include all referenced files in your version control system

## Limitations

- **No templating**: Includes don't support variable substitution or templating
- **Static resolution**: Includes are processed at load time, not dynamically
- **File-based only**: Only supports including from files, not from URLs or other sources

## Error Handling

Common include errors:
- **File not found**: Check the path is correct and file exists
- **Circular includes**: Remove circular dependencies between files
- **Invalid YAML**: Ensure included files contain valid YAML syntax
- **Path security**: Avoid directory traversal (../) in include paths
