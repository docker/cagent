# Environment Variables Required by Examples

This document lists all environment variables required by each example configuration in this directory.

## Quick Setup

1. Copy the `.env.example` file to `.env` in the project root:
   ```bash
   cp .env.example .env
   ```

2. Fill in the API keys for the providers you plan to use

3. Run your chosen example:
   ```bash
   cagent run examples/groq_general.yaml
   ```

---

## Environment Variables by Provider

### AI Model Providers

| Provider | Environment Variable | Get API Key From |
|----------|---------------------|------------------|
| OpenAI | `OPENAI_API_KEY` | https://platform.openai.com/api-keys |
| Anthropic (Claude) | `ANTHROPIC_API_KEY` | https://console.anthropic.com/ |
| Google (Gemini) | `GOOGLE_API_KEY` | https://makersuite.google.com/app/apikey |
| Mistral | `MISTRAL_API_KEY` | https://console.mistral.ai/ |
| Groq | `GROQ_API_KEY` | https://console.groq.com/ |
| OpenRouter | `OPENROUTER_API_KEY` | https://openrouter.ai/keys |
| xAI (Grok) | `XAI_API_KEY` | https://x.ai/ |
| Moonshot (Kimi) | `MOONSHOT_API_KEY` | https://platform.moonshot.cn/ |
| Bytez (Imagen) | `BYTEZ_KEY` | https://bytez.com/ |
| Nebius | `NEBIUS_API_KEY` | https://nebius.ai/ |

### MCP Server Providers

| MCP Server | Environment Variable | Notes |
|-----------|---------------------|-------|
| GitHub | `GITHUB_PERSONAL_ACCESS_TOKEN` | Classic token with repo access |
| Brave Search | `BRAVE_API_KEY` | For search.yaml |
| Kubernetes | `KUBECONFIG` | Path to kubeconfig file (optional) |
| Slack | `SLACK_BOT_TOKEN` | If using Slack MCP |
| Jira | `JIRA_API_TOKEN` | If using Jira MCP |

---

## Examples by Required Environment Variables

### No API Key Required (Tools Only)

- **42.yaml** - No external API (uses built-in tools only)
- **echo-agent.yaml** - No external API
- **contradict.yaml** - No external API

### Requires OPENAI_API_KEY

- basic_agent.yaml
- haiku.yaml
- shell.yaml
- k8s_debugger.yaml (+ KUBECONFIG path)
- structured-output.yaml
- script_shell.yaml
- post_edit.yaml

### Requires ANTHROPIC_API_KEY

- dev-team.yaml
- blog.yaml
- finance.yaml (+ BRAVE_API_KEY for search)
- code.yaml
- dhi/dhi.yaml
- review.yaml
- writer.yaml
- bio.yaml
- doc_generator.yaml
- diag.yaml
- github.yaml (+ GITHUB_PERSONAL_ACCESS_TOKEN)
- github-toon.yaml (+ GITHUB_PERSONAL_ACCESS_TOKEN)
- github_issue_manager.yaml (+ GITHUB_PERSONAL_ACCESS_TOKEN)
- search.yaml (+ BRAVE_API_KEY)

### Requires GROQ_API_KEY

- groq.yaml
- groq_general.yaml
- content_creator.yaml (+ BYTEZ_KEY, optional GITHUB_TOKEN)
- content_creator_verbatim.yaml (+ BYTEZ_KEY, optional GITHUB_TOKEN)
- content_writer.yaml
- content_researcher.yaml
- content_images.yaml (+ BYTEZ_KEY)
- content_social.yaml
- seo.yaml
- interactive_runner.yaml
- multi-code.yaml

### Requires OPENROUTER_API_KEY

- openrouter_general.yaml

### Requires MOONSHOT_API_KEY

- kimi_general.yaml

### Requires MISTRAL_API_KEY

- mistral.yaml

### Requires Multiple Keys

**content_creator.yaml**:
- GROQ_API_KEY (required)
- BYTEZ_KEY (required for image generation)
- GITHUB_PERSONAL_ACCESS_TOKEN (optional, for GitHub integration)

**content_creator_verbatim.yaml**:
- GROQ_API_KEY (required)
- BYTEZ_KEY (required for image generation)
- GITHUB_PERSONAL_ACCESS_TOKEN (optional)

**content_images.yaml**:
- GROQ_API_KEY (required)
- BYTEZ_KEY (required)

**search.yaml**:
- ANTHROPIC_API_KEY (required)
- BRAVE_API_KEY (required for DuckDuckGo alternative)

**finance.yaml**:
- ANTHROPIC_API_KEY (required)
- BRAVE_API_KEY (optional for enhanced search)

**k8s_debugger.yaml**:
- OPENAI_API_KEY (required)
- KUBECONFIG (path to kubeconfig file - see file header for setup)

---

## MCP Docker References

The following examples use MCP servers from the Docker MCP Catalog. These are automatically pulled and run via Docker Desktop's MCP Gateway:

| Example | Docker MCP Servers Used |
|---------|------------------------|
| content_creator.yaml | docker:duckduckgo, docker:github |
| blog.yaml | docker:duckduckgo |
| bio.yaml | docker:duckduckgo, docker:fetch |
| finance.yaml | docker:duckduckgo |
| search.yaml | docker:brave |
| github.yaml | docker:github-official |
| apify.yaml | docker:apify |
| airbnb.yaml | docker:openbnb-airbnb |
| couchbase_agent.yaml | docker:couchbase |
| k8s_debugger.yaml | docker:inspektor-gadget, docker:kubernetes |
| dhi/dhi.yaml | docker:duckduckgo |
| gopher.yaml | docker:ast-grep |

**Note**: Docker MCP servers require Docker Desktop with MCP Gateway support. The catalog is automatically fetched from: https://desktop.docker.com/mcp/catalog/v3/catalog.yaml

---

## MCP Command References

The following examples use MCP servers via direct command execution (must be installed separately):

| Example | Command | Installation |
|---------|---------|--------------|
| content_creator.yaml | rust-mcp-filesystem | `cargo install rust-mcp-filesystem` |
| gopher.yaml | gopls | `go install golang.org/x/tools/gopls@latest` |
| finance.yaml | uvx yfmcp | `pip install yfmcp` |

---

## Special Configurations

### Kubernetes Examples

**k8s_debugger.yaml** requires special setup:

1. Set `OPENAI_API_KEY` in `.env`
2. Edit the YAML file and replace `YOUR_KUBECONFIG_PATH` with:
   - Direct path: `/home/user/.kube/config`
   - Or use environment variable: `${KUBECONFIG}`
3. If using Docker Engine (not Desktop), ensure kubeconfig file owner has uid 1001

### Workflow Examples

The `workflow/` directory contains a 9-stage agent workflow (acg_stage1 through acg_stage9). All stages require:
- GROQ_API_KEY

---

## Validation

To check which environment variables are missing for a specific example, run:

```bash
# This will show which env vars are required but not set
cagent run examples/your-example.yaml --debug
```

The agent will fail with a clear error message indicating which environment variables are missing.

---

## Docker Compose Usage

When using `docker-compose.yml`, ensure your `.env` file is in the project root. The compose file automatically loads environment variables from `.env`.

Example `.env` for the default compose configuration:
```bash
GROQ_API_KEY=your_groq_key_here
TELEMETRY_ENABLED=false
```

---

## Security Best Practices

1. **Never commit** `.env` files to version control
2. Use `.env.example` as a template (no actual keys)
3. Rotate API keys regularly
4. Use read-only or restricted-scope keys when possible
5. For production deployments, use a secure secrets management system (e.g., HashiCorp Vault, AWS Secrets Manager)

---

## Troubleshooting

### Common Issues

**Issue**: `missing environment variable OPENAI_API_KEY required by agent`
**Solution**: Add the key to your `.env` file or export it: `export OPENAI_API_KEY=sk-...`

**Issue**: `MCP server "github-official" requires environment variable GITHUB_PERSONAL_ACCESS_TOKEN`
**Solution**: Create a GitHub Personal Access Token and add to `.env`: `GITHUB_PERSONAL_ACCESS_TOKEN=ghp_...`

**Issue**: `YOUR_KUBECONFIG_PATH` error in k8s_debugger.yaml
**Solution**: Edit the YAML file and replace the placeholder with your actual kubeconfig path

---

For more information about MCP servers and configuration, see:
- [Docker MCP Catalog](https://docs.docker.com/ai/mcp-catalog-and-toolkit/catalog/)
- [Main README](../README.md)
- [Usage Guide](../docs/USAGE.md)
