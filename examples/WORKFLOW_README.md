# Workflow Execution

This directory contains examples of workflow execution in cagent. Workflows allow you to chain multiple agents together in sequential or parallel execution patterns, where agents process and transform data through a defined pipeline.

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

### Parallel Translation Workflow

The `parallel_translation_workflow.yaml` demonstrates parallel execution where multiple agents process the same input concurrently:

1. **source_text** - Generates a technical explanation of Docker containers
2. **Parallel Step** - Three translation agents run simultaneously:
   - **translate_spanish** - Translates to Spanish
   - **translate_french** - Translates to French
   - **translate_japanese** - Translates to Japanese
3. **formatter** - Combines all translations into a formatted output

```bash
./bin/cagent run examples/parallel_translation_workflow.yaml
```

### Parallel Sorting Workflow

The `parallel_sorting_workflow.yaml` shows parallel processing with compute-intensive tasks:

1. **generate_array** - Creates a random array of 100 integers
2. **Parallel Step** - Four sorting agents run concurrently:
   - **bubble_sort** - Sorts using Bubble Sort
   - **insertion_sort** - Sorts using Insertion Sort
   - **merge_sort** - Sorts using Merge Sort
   - **quicksort** - Sorts using QuickSort
3. **analyzer** - Compares and analyzes all sorting results

```bash
./bin/cagent run examples/parallel_sorting_workflow.yaml
```

## How It Works

The `run` command automatically detects workflows by checking if the configuration file contains a `workflow` section. No special command is needed!

### Execution Patterns

Workflows support two execution patterns:

1. **Sequential (`type: agent`)** - Agents run one after another, each receiving the previous agent's output
2. **Parallel (`type: parallel`)** - Multiple agents run concurrently, all receiving the same input, with outputs combined for the next step

## Workflow Configuration

### Basic Structure

#### Sequential Workflow

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

#### Parallel Workflow

```yaml
version: "2"

agents:
  generator:
    model: openai/gpt-4o
    instruction: Generate initial data

  processor1:
    model: openai/gpt-4o
    instruction: Process data using method 1

  processor2:
    model: openai/gpt-4o
    instruction: Process data using method 2

  combiner:
    model: openai/gpt-4o
    instruction: Combine and analyze results

workflow:
  - type: agent
    name: generator
  - type: parallel
    steps:
      - processor1
      - processor2
  - type: agent
    name: combiner
```

### Key Features

1. **Sequential Execution**: Agents run in the order defined in the workflow
2. **Parallel Execution**: Multiple agents process the same input concurrently
3. **Data Piping**: The output of each step becomes the input for the next step
4. **Automatic Context**: The first agent receives instructions to generate initial content, subsequent agents receive the previous output as input
5. **Output Combination**: Parallel step outputs are concatenated in the order specified and passed to the next step
6. **No Root Agent Required**: Workflows don't need a "root" agent - just define the agents used in your workflow steps

### Example Flows

#### Sequential Flow

```
Step 1: story_starter
→ Output: "RoboChef-42 had never encountered a kitchen before..."

Step 2: add_dialogue (receives previous output)
→ Output: "...Chef Lucia approached with curiosity..."

Step 3: add_ending (receives previous output)
→ Output: "...a bright future in the culinary world."
```

#### Parallel Flow

```
Step 1: source_text
→ Output: "Docker containers are lightweight..."

Step 2: Parallel execution (all receive same input)
├─ translate_spanish → "Los contenedores Docker son ligeros..."
├─ translate_french → "Les conteneurs Docker sont légers..."
└─ translate_japanese → "Dockerコンテナは軽量です..."

Step 3: formatter (receives all parallel outputs)
→ Combined output with all three translations formatted
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

- Each agent's output is passed as text to the next step
- For parallel steps, outputs are concatenated in the order specified in the `steps` array
- The workflow stops immediately if any agent fails
- Model overrides can be specified per agent using `--model agent_name=provider/model`
- Supports both `type: agent` (sequential) and `type: parallel` (concurrent) workflow steps
- The final output of the workflow is the output from the last step in the sequence
- Parallel execution provides true concurrency - agents run simultaneously, not sequentially
