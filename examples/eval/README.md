# Agent Evaluation Example

This example demonstrates how to use `cagent eval` to evaluate agent performance using saved conversation sessions.

## Overview

The evaluation system in cagent allows you to:
- Save conversation sessions as JSON files using the `/eval` command during interactive sessions
- Re-run those sessions with the same agent configuration
- Compare the original responses with new responses using metrics like:
  - **Tool Trajectory Score**: Measures how well the agent uses the same tools in the same sequence
  - **ROUGE-1 Score**: Measures text similarity between original and new responses (word overlap)

## Files in this Example

- `agent.yaml` - A simple agent configuration for basic calculations and questions
- `evals/` - Directory containing saved evaluation sessions:
  - `sample-calculation.json` - Example session with a math problem
  - `sample-question.json` - Example session with a general knowledge question

## How to Run the Evaluation

**Prerequisites**: You need appropriate API keys set for the model provider (e.g., `ANTHROPIC_API_KEY` for Claude models).

```console
$ cagent eval agent.yaml ./evals
```

This will output something like:

```console
Eval file: sample-calculation-eval
Tool trajectory score: 1.000000
Rouge-1 score: 0.785430

Eval file: sample-question-eval  
Tool trajectory score: 1.000000
Rouge-1 score: 0.923567
```

## How to Create Evaluation Data

### Method 1: Using the `/eval` Interactive Command

1. Start an interactive session:
   ```bash
   $ cagent run agent.yaml
   ```

2. Have a conversation with the agent:
   ```
   User: What is 25 × 4?
   Agent: I'll calculate that for you:
   
   25 × 4 = 100
   
   The answer is 100.
   ```

3. Save the session for evaluation:
   ```
   /eval
   ```

4. This creates a JSON file in the `evals/` directory with the conversation.

### Method 2: Manual Session Creation

You can manually create evaluation JSON files following the session structure (see existing files in `evals/` for examples).

## Understanding the Scores

- **Tool Trajectory Score**: 1.0 means the agent used exactly the same tools in the same order as the original session. Lower scores indicate different tool usage patterns.

- **ROUGE-1 Score**: Measures word-level overlap between responses. Values closer to 1.0 indicate higher similarity. A score of 0.785430 means about 78.5% word overlap.

## Use Cases

- **Regression Testing**: Ensure agent behavior remains consistent across updates
- **A/B Testing**: Compare different agent configurations or model versions  
- **Performance Monitoring**: Track how agent responses change over time
- **Quality Assurance**: Validate agent outputs against known good responses
