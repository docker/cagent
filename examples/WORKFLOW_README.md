# Sequential Workflow Execution

This directory contains examples of sequential workflow execution in cagent. Workflows allow you to chain multiple agents together, where each agent processes the output from the previous agent.

## Examples

### Story Generation Workflow

The `story_workflow.yaml` file demonstrates a creative writing workflow with three agents:

1. **story_starter** - Writes the opening paragraph of a story about a robot learning to cook
2. **add_dialogue** - Continues the story by adding dialogue between the robot and a chef
3. **add_ending** - Completes the story with a satisfying conclusion

```bash
./bin/cagent run examples/story_workflow.yaml
```

### Product Description Workflow

The `product_description_workflow.yaml` file shows a marketing content workflow:

1. **draft_writer** - Creates an initial product description for a smart water bottle
2. **make_exciting** - Rewrites the description with more engaging language
3. **add_cta** - Adds a compelling call-to-action

```bash
./bin/cagent run examples/product_description_workflow.yaml
```

### Joke Workflow

The `joke_workflow.yaml` demonstrates a simple two-step comedy workflow:

1. **joke_writer** - Creates an original joke
2. **joke_improver** - Enhances the joke with better timing or punchline

```bash
./bin/cagent run examples/joke_workflow.yaml
```

## How It Works

The `run` command automatically detects workflows by checking if the configuration file contains a `workflow` section. No special command is needed!

## Workflow Configuration

### Basic Structure

```yaml
version: "2"

agents:
  agent_name:
    model: openai/gpt-4o
    instruction: |
      Your agent instructions here

workflow:
  - type: agent
    name: agent_name
  - type: agent
    name: next_agent
```

### Key Features

1. **Sequential Execution**: Agents run in the order defined in the workflow
2. **Data Piping**: The output of each agent becomes the input for the next agent
3. **Automatic Context**: The first agent receives instructions to generate initial content, subsequent agents receive the previous output as input
4. **No Root Agent Required**: Workflows don't need a "root" agent - just define the agents used in your workflow steps

### Example Flow

```
Step 1: story_starter
→ Output: "RoboChef-42 had never encountered a kitchen before..."

Step 2: add_dialogue (receives previous output)
→ Output: "...Chef Lucia approached with curiosity..."

Step 3: add_ending (receives previous output)
→ Output: "...a bright future in the culinary world."
```

## Command Options

Workflows support the same runtime configuration flags as regular agent runs:

### Running without TUI (CLI mode)

```bash
./bin/cagent run examples/story_workflow.yaml --tui=false
```

### Model Overrides

Override specific agent models:

```bash
./bin/cagent run examples/story_workflow.yaml \
  --model story_starter=anthropic/claude-sonnet-4-0 \
  --model add_dialogue=openai/gpt-4o
```

### Debug Mode

```bash
./bin/cagent run examples/story_workflow.yaml --debug
```

## Notes

- Each agent's output is passed as text to the next agent
- The workflow stops immediately if any agent fails
- Model overrides can be specified per agent using `--model agent_name=provider/model`
- Currently only supports `type: agent` workflow steps (future: conditions, parallel execution)
- The final output of the workflow is the output from the last agent in the sequence
