---
title: "Cheaper Inference"
description: "Use Cheaper Inference models with Docker Agent."
keywords: docker agent, ai agents, model providers, llm, cheaper inference
weight: 52
canonical: https://docs.docker.com/ai/docker-agent/providers/cheaperinference/
---

_Use Cheaper Inference models with Docker Agent._

## Overview

[Cheaper Inference](https://cheaperinference.com) is an OpenAI-compatible LLM gateway.
It provides access to models from several labs with one API key. Docker Agent
includes built-in support for Cheaper Inference as an alias provider.

## Setup

1. [Create a Cheaper Inference API key](https://cheaperinference.com/signup)
2. Set the environment variable:

   ```bash
   export CHEAPER_INFERENCE_API_KEY=your-api-key
   ```

## Usage

### Inline Syntax

Use a Cheaper Inference model by prefixing its model ID with `cheaperinference/`:

```yaml
agents:
  root:
    model: cheaperinference/gpt-5.4-mini
    description: Assistant using Cheaper Inference
    instruction: You are a helpful assistant.
```

### Named Model

For more control over parameters:

```yaml
models:
  ci_model:
    provider: cheaperinference
    model: gpt-5.4-mini
    temperature: 0.7
    max_tokens: 8192

agents:
  root:
    model: ci_model
    description: Assistant using Cheaper Inference
    instruction: You are a helpful assistant.
```

## Available Models

Cheaper Inference's catalog includes the following exact model ID used by the examples:

| Model ID | Model |
| --- | --- |
| `gpt-5.4-mini` | GPT-5.4 mini |

Model IDs are case-sensitive. Check the [Cheaper Inference model list](https://cheaperinference.com/markets)
or the live `/v1/models` endpoint for current IDs and availability.

Cheaper Inference is not in the models.dev catalog. Docker Agent does not import metadata
from Cheaper Inference's model list, so it has no catalog-derived pricing or context limits
for these models. Attachment capabilities conservatively fall back to text-only;
image and PDF attachments are dropped, with a warning logged once per model.

If you have verified that your chosen model and endpoint accept a particular
attachment type, set `models.<name>.capabilities` to override detection. For example,
`capabilities: {image: true, pdf: false}` enables image attachments only. This is not
a claim that the example model supports them: enable only types the endpoint actually
accepts. See [Attachment Capability Overrides](../../configuration/models/index.md#attachment-capability-overrides)
for all supported flags.

## How It Works

Cheaper Inference is implemented as a built-in alias in Docker Agent:

- **API Type:** OpenAI-compatible Chat Completions (`openai_chatcompletions`)
- **Base URL:** `https://api.cheaperinference.com/v1`
- **Token Variable:** `CHEAPER_INFERENCE_API_KEY`

Because Cheaper Inference can serve open-weight models whose chat templates reject
multiple leading system messages, Docker Agent coalesces its per-source system
messages into one.

## Example: Code Assistant

```yaml
agents:
  coder:
    model: cheaperinference/gpt-5.4-mini
    description: Code assistant using Cheaper Inference
    instruction: |
      You are an expert programmer.
      Write clean code and follow language best practices.
    toolsets:
      - type: filesystem
      - type: shell
      - type: think
```
