# 🤖 `cagent` 🤖

> A powerful, easy to use, customizable multi-agent runtime that orchestrates AI agents with
> specialized capabilities and tools, and the interactions between agents.

![cagent in action](docs/assets/cagent-run.gif)

## ✨ What is `cagent`? ✨

`cagent` lets you create and run intelligent AI agents, where each agent has
specialized knowledge, tools, and capabilities.

Think of it as allowing you to quickly build, share and run a team of virtual experts that
collaborate to solve complex problems for you.

And it's dead easy to use!

⚠️ Note: `cagent` is in active development, **breaking changes are to be expected** ⚠️

### Your First Agent

Example [basic_agent.yaml](/examples/basic_agent.yaml):

Creating agents with cagent is very simple. They are described in a short yaml file, like this one:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
```

Run it in a terminal with `cagent run basic_agent.yaml`.

Many more examples can be found [here](/examples/README.md)!

### Improving an agent with MCP tools

`cagent` supports MCP servers, enabling agents to use a wide variety of external tools and services.  

It supports three transport types: `stdio`, `http` and `sse`.

Giving an agent access to tools via MCP is a quick way to greatly improve its capabilities, the quality of its results and its general useful-ness.

Get started quickly with the [Docker MCP Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/) and [catalog](https://docs.docker.com/ai/mcp-catalog-and-toolkit/catalog/)

Here, we're giving the same basic agent from the example above access to a **containerized** `duckduckgo` mcp server and it's tools by using Docker's MCP Gateway:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
    toolset:
      - type: mcp
        ref: docker:duckduckgo # stdio transport
```

When using a containerized server via the Docker MCP gateway, you can configure any required settings/secrets/authentication using the [Docker MCP Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/#example-use-the-github-official-mcp-server) in Docker Desktop.  

Aside from the containerized MCP severs the Docker MCP Gateway provides, any standard MCP server can be used with cagent!  

Here's an example similar to the above but adding `read_file` and `write_file` tools from the `rust-mcp-filesystem` MCP server:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses. Write your search results to disk.
    toolset:
      - type: mcp
        ref: docker:duckduckgo
      - type: mcp
        command: rust-mcp-filesystem # installed with `cargo install rust-mcp-filesystem`
        args: ["--allow-write", "."]
        tools: ["read_file", "write_file"] # Optional: specific tools only
        env:
          - "RUST_LOG=debug"
```

See [the USAGE docs](./docs/USAGE.md#tool-configuration) for more detailed information and examples

### 🎯 Key Features

- **🏗️ Multi-agent architecture** - Create specialized agents for different domains.
- **🔧 Rich tool ecosystem** - Agents can use external tools and APIs via the MCP protocol.
- **🔄 Smart delegation** - Agents can automatically route tasks to the most suitable specialist.
- **📝 YAML configuration** - Declarative model and agent configuration.
- **💭 Advanced reasoning** - Built-in "think", "todo" and "memory" tools for complex problem-solving.
- **🌐 Multiple AI providers** - Support for OpenAI, Anthropic, Gemini and [Docker Model Runner](https://docs.docker.com/ai/model-runner/).

## 🚀 Quick Start 🚀

### Installation

[Prebuilt binaries](https://github.com/docker/cagent/releases) for Windows, macOS and Linux can be found on the releases page of the [project's GitHub repository](https://github.com/docker/cagent/releases)

Once you've downloaded the appropriate binary for your platform, you may need to give it executable permissions.
On macOS and Linux, this is done with the following command:

```sh
# linux amd64 build example
chmod +x /path/to/downloads/cagent-linux-amd64
```

You can then rename the binary to `cagent` and configure your `PATH` to be able to find it (configuration varies by platform).

### **Set your API keys**

Based on the models you configure your agents to use, you will need to set the corresponding provider API key accordingly,
all theses keys are optional, you will likely need at least one of these, though:

```bash
# For OpenAI models
export OPENAI_API_KEY=your_api_key_here

# For Azure OpenAI (via OpenAI provider)
export AZURE_OPENAI_API_KEY=your_api_key_here

# For Anthropic models
export ANTHROPIC_API_KEY=your_api_key_here

# For Gemini models
export GOOGLE_API_KEY=your_api_key_here
```

> Azure OpenAI configuration details are in the [USAGE](./docs/USAGE.md#azure-openai-via-the-openai-provider) docs

### Run Agents!

```bash
# Run an agent!
cagent run ./examples/pirate.yaml

# or specify a different starting agent from the config, useful for agent teams
cagent run ./examples/pirate.yaml -a root

# or run directly from an image reference here I'm pulling the pirate agent from the creek repository
cagent run creek/pirate
```

### Multi-agent team example

```yaml
agents:
  root:
    model: claude
    description: "Main coordinator agent that delegates tasks and manages workflow"
    instruction: |
      You are the root coordinator agent. Your job is to:
      1. Understand user requests and break them down into manageable tasks
      2. Delegate appropriate tasks to your helper agent
      3. Coordinate responses and ensure tasks are completed properly
      4. Provide final responses to the user
      When you receive a request, analyze what needs to be done and decide whether to:
      - Handle it yourself if it's simple
      - Delegate to the helper agent if it requires specific assistance
      - Break complex requests into multiple sub-tasks
    sub_agents: ["helper"]

  helper:
    model: claude
    description: "Assistant agent that helps with various tasks as directed by the root agent"
    instruction: |
      You are a helpful assistant agent. Your role is to:
      1. Complete specific tasks assigned by the root agent
      2. Provide detailed and accurate responses
      3. Ask for clarification if tasks are unclear
      4. Report back to the root agent with your results

      Focus on being thorough and helpful in whatever task you're given.

models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-0
    max_tokens: 64000
```

You'll find a curated list of agents examples, spread into 3 categories, [Basic](https://github.com/docker/cagent/tree/main/examples#basic-configurations), [Advanced](https://github.com/docker/cagent/tree/main/examples#advanced-configurations) and [multi-agents](https://github.com/docker/cagent/tree/main/examples#multi-agent-configurations) in the `/examples/` directory.

### DMR provider options

When using the `dmr` provider, you can use the `provider_opts` key for DMR runtime-specific (e.g. llama.cpp) options:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      runtime_flags: ["--ngl=33", "--repeat-penalty=1.2", ...]  # or comma/space-separated string
```

The default base_url `cagent` will use for dmr providers is `http://localhost:12434/engines/llama.cpp/v1`. DMR itself might need to be enabled via [Docker Desktop's settings](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-desktop) on MacOS and Windows, and via command line on [Docker CE on Linux](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-engine).

## Quickly generate agents and agent teams with `cagent new`

Using the command `cagent new` you can quickly generate agents or multi-agent teams using a single prompt!  
`cagent` has a built-in agent dedicated to this task.

To use the feature, you must have an Anthropic, OpenAI or Google API key available in your environment, or specify a local model to run with DMR (Docker Model Runner).

You can choose what provider and model gets used by passing the `--model provider/modelname` flag to `cagent new`

If `--model` is unspecified, `cagent new` will automatically choose between these 3 providers in order based on the first api key it finds in your environment.

```sh
export ANTHROPIC_API_KEY=your_api_key_here  # first choice. default model claude-sonnet-4-0
export OPENAI_API_KEY=your_api_key_here     # if anthropic key not set. default model gpt-5-mini
export GOOGLE_API_KEY=your_api_key_here     # if anthropic and openai keys are not set. default model gemini-2.5-flash
```

`--max-tokens` can be specified to override the context limit used.  
When using DMR, the default is 16k to limit memory usage. With all other providers the default is 64k

Example of provider, model and context size overriding:

```sh
# Use GPT-5 via OpenAI
cagent new --model openai/gpt-5

# Use a local model (ai/gemma3-qat:12B) via DMR
cagent new --model dmr/ai/gemma3-qat:12B

# Override the max_tokens used during generation, default is 64k, 16k when using the dmr provider
cagent new --model openai/gpt-5-mini --max-tokens 32000
```

---

```
$ cagent new

------- Welcome to cagent! -------
(Ctrl+C to stop the agent or exit)

What should your agent/agent team do? (describe its purpose):

> I need an agent team that connects to <some-service> and does...
```

## Pushing and pulling agents from Docker Hub

### `cagent push`

Agent configurations can be packaged and shared to Docker Hub using the `cagent push` command

```sh
cagent push ./<agent-file>.yaml namespace/reponame
```

`cagent` will automatically build an OCI image and push it to the desired repository using your Docker credentials

### `cagent pull`

Pulling agents from Docker Hub is also just one `cagent pull` command away.

```sh
cagent pull creek/pirate
```

`cagent` will pull the image, extract the yaml file and place it in your working directory for ease of use.

`cagent run creek.yaml` will run your newly pulled agent

## Usage

More details on the usage and configuration of `cagent` can be found in [USAGE.md](/docs/USAGE.md)

## Telemetry

We track anonymous usage data to improve the tool. See [TELEMETRY.md](/docs/TELEMETRY.md) for details.

## Contributing

Want to hack on `cagent`, or help us fix bugs and build out some features? 🔧

Read the information on how to build from source and contribute to the project in [CONTRIBUTING.md](/docs/CONTRIBUTING.md)

## DogFooding: using `cagent` to code on `cagent`

A smart way to improve `cagent`'s codebase and feature set is to do it with the help of a `cagent` agent!

We have one that we use and that you should use too:

```sh
cd cagent
cagent run ./golang_developer.yaml
```

This agent is an _expert Golang developer specializing in the cagent multi-agent AI system architecture_.

Ask it anything about `cagent`. It can be questions about the current code or about
improvements to the code. It can also fix issues and implement new features!

## Share your feedback

We’d love to hear your thoughts on this project.
You can find us on [Slack](https://dockercommunity.slack.com/archives/C09DASHHRU4)
