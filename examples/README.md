# Configuration Examples Analysis

## **Basic Configurations**

These examples are fairly basic and show you the YAML syntax for writing agents.

Some of these agents use [built-in tools](../docs/USAGE.md#tool-configuration)
like `filesystem`, which grants filesystem access, or `memory`, to allow the agent to store its findings for later use.

| Name                                   | Description/Purpose                    | Filesystem | Shell | Todo | Think | Memory | MCP Servers | Sub-agents |
|----------------------------------------|----------------------------------------|------------|-------|------|-------|--------|-------------|------------|
| [echo-agent.yaml](echo-agent.yaml)     | Simple echo agent                      |            |       |      |       |        |             |            |
| [pirate.yaml](pirate.yaml)             | Pirate-themed assistant                |            |       |      |       |        |             |            |
| [haiku.yaml](haiku.yaml)               | Writes Haikus                          |            |       |      |       |        |             |            |
| [42.yaml](42.yaml)                     | Douglas Adams-style witty AI assistant |            |       |      |       |        |             |            |
| [contradict.yaml](contradict.yaml)     | Contrarian viewpoint provider          |            |       |      |       |        |             |            |
| [silvia.yaml](silvia.yaml)             | Sylvia Plath-inspired poetic AI        |            |       |      |       |        |             |            |
| [script_shell.yaml](script_shell.yaml) | Agent with custom shell commands       |            | ✓     |      |       |        |             |            |
| [mem.yaml](mem.yaml)                   | Humorous AI with persistent memory     | ✓          |       |      |       | ✓      |             |            |
| [diag.yaml](diag.yaml)                 | Log analysis and diagnostics           | ✓          | ✓     |      | ✓     |        |             |            |
| [todo.yaml](todo.yaml)                 | Task manager example                   |            |       | ✓    |       |        |             |            |
| [pythonist.yaml](pythonist.yaml)       | Python programming assistant           | ✓          | ✓     |      |       |        |             |            |
| [fetch_docker.yaml](fetch_docker.yaml) | Web content fetcher and summarizer     |            |       |      |       |        | fetch (builtin) |            |
| [alloy.yaml](alloy.yaml)               | Learning assistant                     |            |       |      |       |        |             |            |
| [dmr.yaml](dmr.yaml)                   | Pirate-themed AI assistant             |            |       |      |       |        |             |            |

## **Advanced Configurations**

These are more advanced examples, most of them involve some sort of MCP server to augment the agent capabilities with powerful custom integrations with third-party services.

| Name                                                   | Description/Purpose                         | Filesystem | Shell | Todo | Think | Memory | MCP Servers                                                                                                                    | Sub-agents |
|--------------------------------------------------------|---------------------------------------------|------------|-------|------|-------|--------|--------------------------------------------------------------------------------------------------------------------------------|------------|
| [bio.yaml](bio.yaml)                                   | Biography generation from internet searches |            |       |      |       |        | [duckduckgo](https://hub.docker.com/mcp/server/duckduckgo/overview), [fetch](https://hub.docker.com/mcp/server/fetch/overview) |            |
| [airbnb.yaml](airbnb.yaml)                             | Airbnb search specialist                    |            |       |      |       |        | `@openbnb/mcp-server-airbnb`                                                                                                   |            |
| [github_issue_manager.yaml](github_issue_manager.yaml) | GitHub Issue Manager                        |            |       |      |       |        | [github-official](https://hub.docker.com/mcp/server/github-official/overview)                                                  |            |
| [github.yaml](github.yaml)                             | GitHub assistance using MCP tools           |            |       |      |       |        | [github-official](https://hub.docker.com/mcp/server/github-official/overview)                                                  |            |
| [review.yaml](review.yaml)                             | Dockerfile review specialist                | ✓          |       |      |       |        |                                                                                                                                |            |
| [code.yaml](code.yaml)                                 | Code analysis and development assistant     | ✓          | ✓     | ✓    |       |        |                                                                                                                                |            |
| [go_packages.yaml](go_packages.yaml)                   | Golang packages expert                      |            |       |      |       |        |                                                                                                                                |            |
| [moby.yaml](moby.yaml)                                 | Moby Project Expert                         |            |       |      |       |        | `gitmcp.io/moby/moby`                                                                                                          |            |
| [image_text_extractor.yaml](image_text_extractor.yaml) | Image text extraction                       | ✓          |       |      |       |        |                                                                                                                                |            |
| [doc_generator.yaml](doc_generator.yaml)               | Documentation generation from codebases     |            | ✓     |      | ✓     |        |                                                                                                                                |            |
| [mcp_generator.yaml](mcp_generator.yaml)               | Generates MCP configurations                |            |       |      |       |        | docker,[duckduckgo](https://hub.docker.com/mcp/server/duckduckgo/overview)                                                     |            |
| [couchbase_agent.yaml](couchbase_agent.yaml)           | Run Database commands using MCP tools       |            |       |      |       |        | docker,[couchbase](https://hub.docker.com/mcp/server/couchbase/overview)                                          |            |
| [notion-expert.yaml](notion-expert.yaml)               | Notion documentation expert using OAuth      |            |       |      |       |        | [notion](https://mcp.notion.com) (uses OAuth)                                                                     |            |

## **Multi-Agent Configurations**

These examples are groups of agents working together. Each of them is specialized for a given task, and usually has some tools assigned to fulfill these tasks.
A coordinator agent usually makes them work together and checks that the work is finished.

| Name                                 | Description/Purpose                     | Filesystem | Shell | Todo | Think | Memory | MCP Servers                                                                    | Sub-agents |
|--------------------------------------|-----------------------------------------|------------|-------|------|-------|--------|--------------------------------------------------------------------------------|------------|
| [blog.yaml](blog.yaml)               | Technical blog writing workflow         |            |       |      | ✓     |        | [duckduckgo](https://hub.docker.com/mcp/server/duckduckgo/overview) | ✓          |
| [dev-team.yaml](dev-team.yaml)       | Development team coordinator            | ✓          | ✓     | ✓    | ✓     | ✓      |                                                                                | ✓          |
| [multi-code.yaml](multi-code.yaml)   | Technical lead and project coordination | ✓          | ✓     | ✓    | ✓     | ✓      |                                                                                | ✓          |
| [writer.yaml](writer.yaml)           | Story writing workflow supervisor       |            |       |      | ✓     |        |                                                                                | ✓          |
| [finance.yaml](finance.yaml)         | Financial research and analysis         |            |       |      | ✓     |        | [duckduckgo](https://hub.docker.com/mcp/server/duckduckgo/overview) | ✓          |
| [shared-todo.yaml](shared-todo.yaml) | Shared todo item manager                |            |       | ✓    |       |        |                                                                                | ✓          |
| [pr-reviewer-bedrock.yaml](pr-reviewer-bedrock.yaml) | PR review toolkit (Bedrock) | ✓          | ✓     |      |       |        |                                                                                | ✓          |
