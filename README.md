# 🤖 `cagent` 🤖

> A powerful, easy to use, customizable multi-agent runtime that orchestrates AI agents with
> specialized capabilities and tools, and the interactions between agents.

![cagent in action](docs/assets/cagent-run.gif)

## ✨ What is `cagent`? ✨

`cagent` lets you create and run intelligent AI agents and teams, where each agent has
specialized knowledge, tools, and capabilities.

Think of it as allowing you to quickly build and run a team of virtual experts that
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

Many examples can be found [here](/examples/README.md)!

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

# For Anthropic models
export ANTHROPIC_API_KEY=your_api_key_here

# For Gemini models
export GOOGLE_API_KEY=your_api_key_here
```

### Run Agents!

```bash
# Run an agent!
cagent run ./examples/pirate.yaml

# or specify a different starting agent from the config, useful for agent teams
cagent run ./examples/pirate.yaml -a root

# or run directly from an image reference
cagent run agentcatalog/pirate
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

## Quickly generate agents and agent teams with `cagent new`

Using the command `cagent new` you can quickly generate agents or multi-agent teams using a single prompt! `cagent` has a built-in agent dedicated to this task.

To use the feature, you must have an Anthropic, OpenAI or Google API key available in your environment.

If `--provider` is unspecified, `cagent new` will automatically choose between these 3 in order based on the first api key it finds in the environment

```sh
export ANTHROPIC_API_KEY=your_api_key_here  # first choice
export OPENAI_API_KEY=your_api_key_here     # if anthropic key not set
export GOOGLE_API_KEY=your_api_key_here     # if anthropic and openai keys are not set
```

The model in use can also be overridden using `--model` (can only be used together with `--provider`)

Example of provider and model overriding:

```sh
cagent new --provider openai --model gpt-5
```

---

```sh
$ cagent new

------- Welcome to cagent! -------
(Ctrl+C to stop the agent or exit)

What should your agent/agent team do? (describe its purpose):

> I need an agent team that connects to <some-service> and does...
```

## Pushing and pulling agents and teams from Docker Hub

### `cagent push`

Agent configurations can be packaged and shared to Docker Hub using the `cagent push` command

```sh
cagent push ./<agent-file>.yaml namespace/reponame
```

`cagent` will automatically build an OCI image and push it to the desired repository using your Docker credentials

### `cagent pull`

Pulling agents/teams from Docker Hub is also just one `cagent pull` command away.

```sh
cagent pull agentcatalog/pirate
```

`cagent` will pull the image, extract the yaml file and place it in your working directory for ease of use.

`cagent run agentcatalog_pirate.yaml` will run your newly pulled agent

## Usage

More details on the usage and configuration of `cagent` can be found in [USAGE.md](/docs/USAGE.md)

## Telemetry

We track anonymous usage data to improve the tool. See [TELEMETRY.md](/docs/TELEMETRY.md) for details.

## Contributing

Want to hack on `cagent`, or help us fix bugs and build out some features? 🔧

Read the information on how to build from source and contribute to the project in [CONTRIBUTING.md](/docs/CONTRIBUTING.md)
