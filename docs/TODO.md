# Documentation TODO

## New Pages

- [x] **Go SDK** — Not documented anywhere. The `examples/golibrary/` directory shows how to use cagent as a Go library. Needs a dedicated page. *(Completed: pages/guides/go-sdk.html)*
- [x] **Hooks** — `hooks` agent config (`pre_tool_use`, `post_tool_use`, `session_start`, `session_end`) is a significant feature with no documentation page. Covers running shell commands at various agent lifecycle points. *(Completed: pages/configuration/hooks.html)*
- [x] **Permissions** — Top-level `permissions` config with `allow`/`deny` glob patterns for tool call approval. Mentioned briefly in TUI page but has no dedicated reference. *(Completed: pages/configuration/permissions.html)*
- [x] **Sandbox Mode** — Shell tool `sandbox` config runs commands in Docker containers. Includes `image` and `paths` (bind mounts with `:ro` support). Not documented. *(Completed: pages/configuration/sandbox.html)*
- [x] **Structured Output** — Agent-level `structured_output` config (name, description, schema, strict). Forces model responses into a JSON schema. Not documented. *(Completed: pages/configuration/structured-output.html)*
- [x] **Model Routing** — Model-level `routing` config with rules that route requests to different models based on example phrases (rule-based router). *(Completed: pages/configuration/routing.html)*
- [x] **Custom Providers (top-level `providers` section)** — The `providers` top-level config key for defining reusable provider definitions with `api_type`, `base_url`, `token_key`. *(Completed: Added to pages/configuration/overview.html)*
- [x] **LSP Tool** — `type: lsp` toolset that provides Language Server Protocol integration (diagnostics, code actions, references, rename, etc.). Not documented anywhere. *(Completed: pages/tools/lsp.html)*
- [x] **User Prompt Tool** — `type: user_prompt` toolset that allows agents to ask the user for input mid-conversation. Not documented. *(Completed: pages/tools/user-prompt.html)*
- [x] **API Tool** — `api_config` on toolsets for defining HTTP API tools with endpoint, method, headers, args, and output_schema. Not documented. *(Completed: pages/tools/api.html)*
- [ ] **Branching Sessions** — TUI feature (v1.20.6) allowing editing previous messages to create branches. Mentioned in TUI page but could use more detail.

## Missing Details in Existing Pages

### Configuration > Agents (`agents.html`)

- [x] `welcome_message` — Agent property not listed in schema or properties table *(Added)*
- [x] `handoffs` — Agent property for listing agents that can be handed off to (different from `sub_agents`). Not documented. *(Added)*
- [x] `add_prompt_files` — Agent property for including additional prompt files. *(Added)*
- [x] `add_description_parameter` — Agent property. *(Added)*
- [x] `code_mode_tools` — Agent property. *(Added)*
- [x] `hooks` — Agent property. Not shown in schema or properties table. *(Added with link to hooks page)*
- [x] `structured_output` — Agent property. Not shown in schema or properties table. *(Added with link to structured-output page)*
- [x] `defer` — Tool deferral configuration. *(Added with examples)*
- [x] `permissions` — Permission configuration. *(Added with link to permissions page)*
- [x] `sandbox` — Sandbox mode configuration. *(Added with link to sandbox page)*

### Configuration > Tools (`tools.html`)

- [x] **LSP toolset** (`type: lsp`) — Language Server Protocol integration. *(Added with link to dedicated page)*
- [x] **User Prompt toolset** (`type: user_prompt`) — User input collection. *(Added with link to dedicated page)*
- [x] **API toolset** (`type: api`) — HTTP API tools. *(Added with link to dedicated page)*
- [x] **Handoff toolset** (`type: handoff`) — A2A agent delegation. *(Added)*
- [x] **A2A toolset** (`type: a2a`) — Toolset for connecting to remote A2A agents with `name` and `url`. *(Added)*
- [x] **Shared todo** (`shared: true`) — Todo toolset option for sharing todos across agents. *(Added)*
- [x] **Filesystem `post_edit`** — Post-edit commands that run after file edits (e.g., auto-format). *(Added)*
- [x] **Filesystem `ignore_vcs`** — Option to ignore VCS (.gitignore) files. *(Added)*
- [x] **Shell `env`** — Environment variables for shell/script/mcp/lsp tools. *(Added)*
- [x] **Fetch `timeout`** — Fetch tool timeout configuration. *(Added)*
- [x] **Script tool format** — Updated to show the correct `shell` map format with args, required, env, working_dir. *(Fixed)*
- [x] **MCP `config`** — The `config` field on MCP toolsets. *(Added)*

### Configuration > Models (`models.html`)

- [x] `track_usage` — Model property to track token usage. *(Added)*
- [x] `token_key` — Model property for specifying the env var holding the API token. *(Added)*
- [x] `routing` — Model property for rule-based routing. *(Added with link to routing page)*
- [x] `base_url` — Added examples showing how to use it with custom/self-hosted endpoints. *(Added)*

### Configuration > Overview (`overview.html`)

- [x] `metadata` — Top-level config section (author, license, description, readme, version). *(Added)*
- [x] `permissions` — Top-level config section. *(Added link to permissions page)*
- [x] `providers` — Top-level section. *(Added full documentation)*
- [x] Config `version` field — Current version is "5" but not documented what it means or how migration works. *(Added)*
- [x] **Advanced configuration cards** — Added cards linking to Hooks, Permissions, Sandbox, and Structured Output pages. *(Done)*

### Features > CLI (`cli.html`)

- [x] `--prompt-file` flag — Explanation of how it works (includes file contents as system context). *(Added)*
- [x] `--session` with relative references — e.g., `-1` for last session, `-2` for second to last. *(Added)*
- [x] Multi-turn conversations in `cagent exec` — Added example. *(Added)*
- [x] Queueing multiple messages: `cagent run question1 question2 ...` *(Added)*
- [x] `cagent eval` flags — Added examples with flags. *(Added)*
- [x] `cagent build` command — *(Added)*
- [ ] `--exit-on-stdin-eof` flag — Hidden flag, low priority.
- [ ] `--keep-containers` flag for eval — Already documented in eval page.

### Features > TUI (`tui.html`)

- [x] Ctrl+R reverse history search — *(Added with dedicated section)*
- [x] `/title` command for renaming sessions — *(Already documented)*
- [x] `/think` command to toggle thinking at runtime — *(Already documented)*
- [x] Custom themes and hot-reloading — *(Already documented)*
- [x] Ctrl+Z to suspend TUI — *(Added)*
- [x] Ctrl+L audio listening shortcut — *(Added)*
- [x] Ctrl+X to clear queued messages — *(Added)*
- [x] Permissions view dialog — *(Mentioned)*
- [x] Model picker / switching during session — *(Already documented)*
- [ ] Branching sessions (edit previous messages) — Mentioned but could have more detail.
- [ ] Double-click title to edit — Minor feature.

### Features > Skills (`skills.html`)

- [x] Skill invocation via slash commands — *(Added)*
- [x] Recursive `~/.agents/skills` directory support — *(Clarified in table)*

### Features > Evaluation (`evaluation.html`)

- [x] `--keep-containers` flag — *(Already documented)*
- [x] Session database produced for investigation — *(Added note)*
- [x] Debugging tip for failed evals — *(Added callout)*

### Providers

- [x] **Mistral** — Listed as built-in alias but has no dedicated page or usage examples. *(Completed: pages/providers/mistral.html)*
- [x] **xAI (Grok)** — *(Completed: pages/providers/xai.html)*
- [x] **Nebius** — *(Completed: pages/providers/nebius.html)*
- [x] **Ollama** — Can be used via custom providers. *(Completed: pages/providers/local.html - covers Ollama, vLLM, LocalAI)*

### Features > RAG (`rag.html`)

- [x] `respect_vcs` option — *(Added with default value)*
- [x] `return_full_content` results option — *(Added)*
- [ ] Code-aware chunking (`code_aware: true`) with tree-sitter — Partially documented, the option is shown in examples.

### Community > Troubleshooting

- [x] Common errors: context window exceeded, max iterations reached, model fallback behavior. *(Added)*
- [x] Debugging with `--debug` and `--log-file` — *(Already documented)*

## Tips & Best Practices

- [x] *(Completed: pages/guides/tips.html)* - Comprehensive tips page covering:
  - [x] **Tip: Using `--yolo` mode** — Auto-approve all tool calls. Security implications and when it's appropriate.
  - [x] **Tip: Environment variable interpolation in commands** — Commands support `${env.VAR}` and `${env.VAR || 'default'}` JavaScript template syntax.
  - [x] **Tip: Fallback model strategy** — Best practices for choosing fallback models.
  - [x] **Tip: Deferred tools for performance** — Use `defer: true` to load tools only when needed.
  - [x] **Tip: Combining handoffs and sub_agents** — Explain the difference.
  - [x] **Tip: Using the `auto` model** — The special `auto` model value for automatic model selection.
  - [x] **Tip: Model aliases and pinning** — cagent automatically resolves model aliases to pinned versions.
  - [x] **Tip: User-defined default model** — Users can define their own default model in global configuration. *(Added)*
  - [x] **Tip: Usage on Github** - Example of the PR reviewer. *(Added)*

## Navigation Updates

- [x] Added Model Routing to Configuration section
- [x] Added xAI (Grok) and Nebius to Model Providers section
- [x] Added Guides section with Tips & Best Practices and Go SDK

## Remaining Low-Priority Items

- [ ] Branching sessions — More detailed documentation (currently mentioned)
- [ ] Double-click title to edit in TUI — Minor feature
- [ ] `--exit-on-stdin-eof` flag — Hidden flag for integration
- [ ] Code-aware chunking detail — Already shown in examples
