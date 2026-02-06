# 🤖 Docker `cagent` 🤖

> A powerful, easy-to-use, customizable multi-agent runtime that orchestrates AI
> agents with specialized capabilities and tools, and the interactions between
> agents.

![cagent in action](docs/demo.gif)

## ✨ What is Docker `cagent`? ✨

Docker `cagent` lets you create and run intelligent AI agents, where each agent has
specialized knowledge, tools and capabilities.

Think of it as allowing you to quickly build, share and run a team of virtual
experts that collaborate to solve complex problems for you.

**And it's dead easy to use! In most cases, you don't need to write a single line of code.**

## 🎯 Key Features

- **🏗️ Multi-agent architecture** - Create specialized agents for different
  domains.
- **🔧 Rich tool ecosystem** - Agents can use external tools and APIs via the
  MCP protocol.
- **🔄 Smart delegation** - Agents can automatically route tasks to the most
  suitable specialist.
- **📝 YAML configuration** - Declarative model and agent configuration.
- **💭 Advanced reasoning** - Built-in "think", "todo" and "memory" tools for
  complex problem-solving.
- **🔍 RAG (Retrieval-Augmented Generation)** - Pluggable retrieval strategies
  (BM25, chunked-embeddings, semantic-embeddings) with hybrid retrieval, result fusion and reranking support.
- **🌐 AI provider agnostic** - Support for OpenAI, Anthropic, Gemini, AWS
  Bedrock, xAI, Mistral, Nebius and [Docker Model
  Runner](https://docs.docker.com/ai/model-runner/).
- **🔀 Runtime model switching** - Change models on-the-fly during a session
  with the `/model` command, with automatic persistence across session reloads.

## Your First Agent

Creating agents with Docker `cagent` is straightforward. They are described in a `.yaml`
file, like this one:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
```

+ Follow the installation instructions.
+ Create a`basic_agent.yaml` file with the above content.
+ Run it in a terminal with `cagent run basic_agent.yaml`.

Many more examples can be found [here](/examples/README.md)!

## 🚀 Installation 🚀

### It ships with Docker Desktop!

Starting with [Docker Desktop 4.49.0](https://docs.docker.com/desktop/release-notes/#4490),
Docker `cagent` is automatically installed.

```sh
$ cagent run default "Hi, what can you do for me?"
```

### Using Homebrew

As an alternative, it's also on [homebrew](https://brew.sh/):

```sh
$ brew install cagent
```

### Using binary releases

Finally, [Prebuilt binaries](https://github.com/docker/cagent/releases) for Windows,
macOS and Linux can be found on the release page of the [project's GitHub
repository](https://github.com/docker/cagent/releases).

Once you've downloaded the appropriate binary for your platform, you may need to
give it executable permissions. On macOS and Linux, this is done with the
following command:

```sh
# linux amd64 build example
chmod +x /path/to/downloads/cagent-linux-amd64
```

You can then rename the binary to Docker `cagent` and configure your `PATH` to be able
to find it (configuration varies by platform).

### **Set your API keys**

Based on the models you configure your agents to use, you will need to set the
corresponding provider API key accordingly, all these keys are optional, you
will likely need at least one of these, though:

```bash
export OPENAI_API_KEY=your_api_key_here              # For OpenAI models
export ANTHROPIC_API_KEY=your_api_key_here           # For Anthropic models
export GOOGLE_API_KEY=your_api_key_here              # For Gemini models
export AWS_BEARER_TOKEN_BEDROCK=your_api_key_here    # For AWS Bedrock available models
export XAI_API_KEY=your_api_key_here                 # For xAI models
export NEBIUS_API_KEY=your_api_key_here              # For Nebius models
export MISTRAL_API_KEY=your_api_key_here             # For Mistral models
```

**Note:** For the different AWS Bedrock authentication options, take a look [here](docs/USAGE.md#aws-bedrock-provider-usage).

### Run Agents!

```bash
# Run an agent!
cagent run ./examples/pirate.yaml

# or specify a different starting agent from the config, useful for agent teams
cagent run ./examples/gopher.yaml -a planner

# or run directly from an image reference.
# Here we're pulling the pirate agent from the creek repository
cagent run creek/pirate
```

Many more examples can be found [here](/examples/README.md)!

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
    model: claude-sonnet-4-5
    max_tokens: 64000
```

You'll find a curated list of agents examples, spread into 3 categories,
[Basic](https://github.com/docker/cagent/tree/main/examples#basic-configurations),
[Advanced](https://github.com/docker/cagent/tree/main/examples#advanced-configurations)
and
[multi-agents](https://github.com/docker/cagent/tree/main/examples#multi-agent-configurations)
in the `/examples/` directory.

### DMR (Docker Model Runner) provider options

When using the `dmr` provider, you can use the `provider_opts` key for DMR
runtime-specific (e.g. llama.cpp/vllm) options and speculative decoding:

```yaml
models:
  local-qwen:
    provider: dmr
    model: ai/qwen3
    max_tokens: 8192
    provider_opts:
      # general flags passed to the underlying model runtime
      runtime_flags: ["--ngl=33", "--repeat-penalty=1.2", ...] # or comma/space-separated string
      # speculative decoding for faster inference
      speculative_draft_model: ai/qwen3:1B
      speculative_num_tokens: 5
      speculative_acceptance_rate: 0.8
```

The default base_url Docker `cagent` will use for DMR providers is
`http://localhost:12434/engines/llama.cpp/v1`. DMR itself might need to be
enabled via [Docker Desktop's
settings](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-desktop)
on macOS and Windows, and via the command-line on [Docker CE on
Linux](https://docs.docker.com/ai/model-runner/get-started/#enable-dmr-in-docker-engine).

See the [DMR Provider documentation](docs/USAGE.md#dmr-docker-model-runner-provider-usage) for more details on runtime flags and speculative decoding options.

### Improving an agent with MCP tools

Docker `cagent` supports MCP servers, enabling agents to use a wide variety of external
tools and services.

It supports all transport types: `stdio`, `http` and `sse`.

Giving an agent access to tools via MCP is a good way to greatly improve its
capabilities, the quality of its results and its general usefulness.

Get started with the [Docker MCP
Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/) and
[catalog](https://docs.docker.com/ai/mcp-catalog-and-toolkit/catalog/)

Here, we're giving the same basic agent from the example above access to a
**containerized** `duckduckgo` mcp server and its tools by using Docker's MCP
Gateway:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
```

When using a containerized server via the `Docker MCP gateway`, you can configure
any required settings/secrets/authentication using the [Docker MCP
Toolkit](https://docs.docker.com/ai/mcp-catalog-and-toolkit/toolkit/#example-use-the-github-official-mcp-server)
in Docker Desktop.

Aside from the containerized MCP servers the `Docker MCP Gateway` provides, any
standard MCP server can be used with Docker `cagent`!

Here's an example similar to the above but adding `read_file` and `write_file`
tools from the `rust-mcp-filesystem` MCP server:

```yaml
agents:
  root:
    model: openai/gpt-5-mini
    description: A helpful AI assistant
    instruction: |
      You are a knowledgeable assistant that helps users with various tasks.
      Be helpful, accurate, and concise in your responses. Write your search results to disk.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
      - type: mcp
        command: rust-mcp-filesystem # installed with `cargo install rust-mcp-filesystem`
        args: ["--allow-write", "."]
        tools: ["read_file", "write_file"] # Optional: specific tools only
        env:
          - "RUST_LOG=debug"
```

See [the USAGE docs](./docs/USAGE.md#tool-configuration) for more detailed
information and examples

### Exposing agents as MCP tools

Docker `cagent` can expose agents as MCP tools via the `cagent mcp` command,
allowing other MCP clients to use your agents over MCP.

Each agent in your configuration becomes an MCP tool with its description.

```bash
# Start MCP server with local file
cagent mcp ./examples/dev-team.yaml

# Or use an OCI artifact
cagent mcp agentcatalog/pirate
```

This exposes each agent as a tool (e.g., `root`, `designer`, `awesome_engineer`) that MCP clients can call:

```json
{
  "method": "tools/call",
  "params": {
    "name": "designer",
    "arguments": {
      "message": "Design a login page"
    }
  }
}
```

See [MCP Mode documentation](./docs/MCP-MODE.md) for detailed instructions on exposing your agents through MCP with Claude Desktop, Claude Code, and other MCP clients.

### RAG (Retrieval-Augmented Generation)

Give your agents access to your documents with Docker `cagent`'s modular RAG system. It supports multiple retrieval strategies that can be used individually or combined for hybrid search.

### Quick RAG Example

```yaml
models:
  embedder:
    provider: openai
    model: text-embedding-3-small

rag:
  my_knowledge_base:
    docs: [./documents, ./pdfs]
    strategies:
      - type: chunked-embeddings
        model: embedder
        threshold: 0.5
        chunking:
          size: 1000
          overlap: 100
    results:
      limit: 5

agents:
  root:
    model: openai/gpt-4o
    instruction: |
      You are an assistant with access to an internal knowledge base.
      Use the knowledge base to gather context before answering user questions
    rag: [my_knowledge_base]
```

#### Hybrid Retrieval (Chunked-Embeddings + BM25)

Combine semantic search (chunked-embeddings) with keyword search (BM25) for best results:

```yaml
rag:
  hybrid_search:
    docs: [./shared_docs]
    
    strategies:
      - type: chunked-embeddings
        model: embedder
        threshold: 0.5
        limit: 20
        chunking:
          size: 1000
          overlap: 100
      
      - type: bm25
        k1: 1.5
        b: 0.75
        threshold: 0.3
        limit: 15
        chunking:
          size: 1000
          overlap: 100
    
    results:
      fusion:
        strategy: rrf  # Reciprocal Rank Fusion
        k: 60
      deduplicate: true
      limit: 5

agents:
  root:
    model: openai/gpt-4o
    rag: [hybrid_search]
```

**Features:**
- **Multiple strategies**: Vector embeddings, semantic embeddings, BM25 (keyword), or combinations
- **Parallel execution**: Strategies run concurrently for fast results
- **Pluggable fusion**: RRF, weighted, or max score combining
- **Result reranking**: Re-score results with specialized models for improved relevance
- **Per-strategy configuration**: Different thresholds, limits, and documents
- **Auto file watching**: Reindex automatically on file changes

#### Result Reranking

Improve search quality by re-scoring retrieved results with a reranking model:

```yaml
rag:
  knowledge_base:
    docs: [./documents]
    strategies:
      - type: chunked-embeddings
        model: openai/text-embedding-3-small
        limit: 20  # Retrieve more candidates for reranking
    
    results:
      reranking:
        model: openai/gpt-4.1-mini   # Any chat model or DMR reranker
        top_k: 10                   # Only rerank top 10 (optional)
        threshold: 0.3              # Filter low-scoring results (optional)
        criteria: |                 # Domain-specific relevance guidance (optional, not used with DMR reranking specific models)
          Prioritize recent documentation and practical examples.
          Documents from official sources are more relevant.
      limit: 5  # Final top results after reranking
```

**Supported providers:** DMR (native `/rerank` endpoint), OpenAI, Anthropic, Gemini (via structured outputs)  
**Note:** Temperature defaults to 0.0 for more deterministic scoring when not explicitly set.

See the [RAG documentation in USAGE.md](docs/USAGE.md#rag-configuration) for complete details, examples, and debugging guides.

## Quickly generate agents and agent teams with `cagent new`

Using the command `cagent new` you can quickly generate agents or multi-agent
teams using a single prompt!  
`cagent` has a built-in agent dedicated to this task.

To use the feature, you must have an Anthropic, OpenAI or Google API key
available in your environment or specify a local model to run with DMR (Docker
Model Runner).

You can choose what provider and model gets used by passing the `--model
provider/modelname` flag to `cagent new`

If `--model` is unspecified, `cagent new` will automatically choose between
these three providers in order based on the first api key it finds in your
environment.

```sh
export ANTHROPIC_API_KEY=your_api_key_here  # first choice. default model claude-sonnet-4-5
export OPENAI_API_KEY=your_api_key_here     # if anthropic key not set. default model gpt-5-mini
export GOOGLE_API_KEY=your_api_key_here     # if anthropic and openai keys are not set. default model gemini-2.5-flash
```

`--max-tokens` can be specified to override the context limit used.  
When using DMR, the default is 16k to limit memory usage. With all other
providers the default is 64k

`--max-iterations` can be specified to override how many times the agent is
allowed to loop when doing tool calling etc. When using DMR, the default is set
to 20 (small local models have the highest chance of getting confused and
looping endlessly). For all other providers, the default is 0 (unlimited).

Example of provider, model, context size and max iterations overriding:

```sh
# Use GPT-5 via OpenAI
cagent new --model openai/gpt-5

# Use a local model (ai/gemma3-qat:12B) via DMR
cagent new --model dmr/ai/gemma3-qat:12B

# Override the max_tokens used during generation, default is 64k, 16k when using the dmr provider
cagent new --model openai/gpt-5-mini --max-tokens 32000

# Override max_iterations to limit how much the model can loop autonomously when tool calling
cagent new --model dmr/ai/gemma3n:2B-F16 --max-iterations 15
```

---

```
$ cagent new

------- Welcome to cagent! -------
(Ctrl+C to stop the agent and exit)

What should your agent/agent team do? (describe its purpose):

> I need an agent team that connects to <some-service> and does...
```

## Pushing and pulling agents from Docker Hub

### `cagent push`

Agent configurations can be packaged and shared to Docker Hub using the `cagent
push` command

```sh
cagent push ./<agent-file>.yaml namespace/reponame
```

Docker `cagent` will automatically build an OCI image and push it to the desired
repository using your Docker credentials

### `cagent pull`

Pulling agents from Docker Hub is also just one `cagent pull` command away.

```sh
cagent pull creek/pirate
```

`cagent` will pull the image, extract the .yaml file and place it in your working
directory for ease of use.

`cagent run creek.yaml` will run your newly pulled agent

## Usage

More details on the usage and configuration of Docker `cagent` can be found in
[USAGE.md](/docs/USAGE.md)

## Telemetry

We track anonymous usage data to improve the tool. See
[TELEMETRY.md](/docs/TELEMETRY.md) for details.

## Contributing

Want to hack on Docker `cagent`, or help us fix bugs and build out some features? 🔧

Read the information on how to build from source and contribute to the project
in [CONTRIBUTING.md](/docs/CONTRIBUTING.md)

## DogFooding: using Docker `cagent` to code on Docker `cagent`

A smart way to improve Docker `cagent`'s codebase and feature set is to do it with the
help of a Docker `cagent` agent!

We have one that we use and that you should use too:

```sh
cd cagent
cagent run ./golang_developer.yaml
```

This agent is an _expert Golang developer specializing in the Docker `cagent`
multi-agent AI system architecture_.

Ask it anything about Docker `cagent`. It can be questions about the current code or
about improvements to the code. It can also fix issues and implement new
features!

## Share your feedback

We’d love to hear your thoughts on this project. Feel free to join the [Docker Community Slack workspace](http://dockr.ly/comm-slack) and the [cagent Slack channel](https://dockercommunity.slack.com/archives/C09DASHHRU4).
