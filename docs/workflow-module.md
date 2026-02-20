# Cagent Workflow Module

This document designs the three core workflow execution patterns in docker/cagent and answers implementation-specific use cases.

## Overview

Workflows define **declarative pipelines** of agents and conditions. Execution is driven by the runtime: each step runs an agent (or evaluates a condition), and step outputs flow to the next step according to the pattern.

## 1. Sequential Step Execution

**Description:** Agents execute one after another in a linear chain. Each agent's output becomes available as input context for the next agent.

**Example:**

```yaml
workflow:
  - type: agent
    name: generator
  - type: agent
    name: translator
  - type: agent
    name: publisher
```

**Behavior:**

- `generator` runs first and completes.
- `translator` receives `generator`'s output as context in its prompt and processes it.
- `publisher` receives `translator`'s output as context and finalizes.

**Output propagation:** Each step automatically receives **all prior step outputs** injected as context into its user message. The executor collects the last assistant message content from each completed step and formats it as a structured context block:

```
--- Prior Step Outputs ---

[step_id (agent: generator)]:
<generator's output>

--- End Prior Step Outputs ---

<original user prompt>
```

Outputs are also accessible via template expressions: `{{ $steps.<step_id>.output }}`.

---

## 2. Conditional Branching & Loops

**Description:** The workflow branches based on condition evaluation. When a condition's branch routes back to an earlier step (by step ID or index), it creates a **loop**. Conditions reference step outputs via templates.

**Example:**

```yaml
workflow:
  - id: gen
    type: agent
    name: generator
  - id: trans
    type: agent
    name: translator
  - id: qa_check
    type: condition
    name: qa_check
    condition: "{{ $steps.qa.output.is_approved }}"
    true:
      - type: agent
        name: publisher
    false:
      - id: back_to_trans
        type: agent
        name: translator
  - id: qa
    type: agent
    name: qa_agent
```

**Behavior:**

- After `translator`, the `qa_check` condition runs (using `qa_agent` output when referenced by `$steps.qa.output`).
- If `is_approved == true`: workflow proceeds to `publisher`.
- If `is_approved == false`: workflow routes to the step that runs `translator` again (retry loop).

**Condition schema:** Conditions are evaluated after the step(s) that produce the referenced output. The condition expression uses a small expression language (e.g. `{{ $steps.<id>.output.<path> }}`) and must resolve to a boolean. Schema validation ensures referenced step IDs exist and that structured output (e.g. `is_approved`) is declared where needed (e.g. via agent `structured_output`).

---

## 3. Parallel Step Execution

**Description:** Multiple steps run concurrently. The workflow waits for **all** parallel steps to complete before moving to the next sequential step.

**Example:**

```yaml
workflow:
  - type: parallel
    id: par_gen
    steps:
      - id: gen_1
        type: agent
        name: generator
      - id: gen_2
        type: agent
        name: generator
  - type: agent
    name: translator
```

**Behavior:**

- Two `generator` agents run concurrently in separate goroutines.
- Both must complete before `translator` starts.
- `translator` receives **outputs from all parallel steps** as context in its prompt (see "Output structure from parallel steps" below).

**Concurrency safety:** Parallel steps use two mechanisms to avoid races:
1. A **`runnerMu` mutex** on the executor serializes `SetCurrentAgent` + `RunStream` calls so each goroutine's internal runtime captures the correct agent name.
2. Each parallel goroutine uses a **sub-session** (`ParentID` set), causing `PersistentRuntime` to skip all SQLite persistence for those sessions.
3. The **`SQLiteSessionStore`** has a `sync.Mutex` on all write methods as an additional safety net.

**Error handling:** If **any** agent in a parallel block fails, the **entire workflow** fails immediately (all-or-nothing). No partial success; this keeps data consistency and avoids downstream agents seeing incomplete data.

---

## Use Case: How deep can loops go? (max iteration count)

**Answer:** Loops are bounded by a **max loop iterations** setting.

- **Config:** `workflow.max_loop_iterations` (default: `100`). Optional per-workflow override: `workflow.overrides.max_loop_iterations`.
- **Semantics:** A "loop" is one execution of a cycle (e.g. trans → qa_check → trans). The executor counts how many times the **same step ID** has been executed in a cycle. When that count reaches `max_loop_iterations`, the workflow fails with a deterministic error (e.g. `workflow: max loop iterations exceeded (step: trans, limit: 100)`).
- **Scope:** The count is per logical loop (per back-edge in the workflow graph), not global across all steps.

This prevents infinite loops while allowing retries (e.g. QA reject → translator) up to a clear limit.

---

## Use Case: Can we nest parallel blocks?

**Answer:** **Yes.** Parallel steps are just steps; their children can be any step type, including another `parallel`.

**Example:**

```yaml
workflow:
  - type: parallel
    id: outer
    steps:
      - type: agent
        name: generator
      - type: parallel
        id: inner
        steps:
          - type: agent
            name: researcher
          - type: agent
            name: summarizer
  - type: agent
    name: publisher
```

**Behavior:** `generator` runs in parallel with the inner parallel block (`researcher` and `summarizer`). All three agent outputs are available to `publisher` (see output structure below). Failure of any of the three fails the whole workflow.

---

## Use Case: How are outputs from multiple parallel agents structured when passed to the next step?

**Answer:** Outputs from a parallel block are passed as a **keyed map** by step ID (and optionally by index for backwards compatibility).

**Structure:**

```json
{
  "steps": {
    "gen_1": { "output": "<last assistant message content>", "agent": "generator" },
    "gen_2": { "output": "<last assistant message content>", "agent": "generator" }
  },
  "order": ["gen_1", "gen_2"]
}
```

- **Next step input:** The next agent receives all parallel outputs injected as context in its user message:
  ```
  --- Prior Step Outputs ---

  [par_gen/gen_1 (agent: generator)]:
  <generator 1 output>

  [par_gen/gen_2 (agent: generator)]:
  <generator 2 output>

  --- End Prior Step Outputs ---

  <original user prompt>
  ```
- **Templates:** In conditions or in agent instructions, parallel outputs are accessed as:
  - `{{ $steps.par_gen.outputs.gen_1.output }}` — output of parallel step `gen_1`
  - `{{ $steps.par_gen.outputs.gen_2.output }}`
  - Or by index: `{{ $steps.par_gen.outputs[0].output }}` (using `order` for deterministic indexing).

So: **one structured object** keyed by step ID (and ordered list for index-based access), passed as context to the next step.

---

## Use Case: What retry behavior exists for failed steps?

**Answer:** Configurable **per-step retry** with optional backoff.

- **Config:** On any step (agent or parallel block):
  - `retry.max_attempts` (default: 0 = no retry)
  - `retry.backoff` (optional): `fixed` (e.g. 1s) or `exponential` (e.g. 1s, 2s, 4s)
  - `retry.on` (optional): list of error patterns or exit conditions to retry on (e.g. `["timeout", "rate_limit"]`); if absent, retry on any error.

**Behavior:**

- A **step** (single agent or whole parallel block) is retried up to `max_attempts` times on failure.
- After exhausting retries, the **workflow** fails (no partial success for parallel).
- Retries are **transparent** to downstream steps: they only see the final success or the workflow fails.

**Loops vs retries:** Loops (condition → back to earlier step) are **logical workflow branches**. Retries are **transient error handling** for the same step. Both can be used: e.g. retry a step 2 times, then continue to a condition that may send the workflow back to an earlier step (e.g. QA reject → translator).

---

## Use Case: How do we access outputs from parallel steps in subsequent agents?

**Answer:** Two mechanisms:

1. **Automatic context injection:** The executor injects a **context blob** into the next step's session (e.g. as a system or user message) containing:
   - `$steps.<parallel_id>.outputs` — the keyed map of step ID → `{ output, agent }`
   - `$steps.<parallel_id>.order` — deterministic order for index-based access.

2. **Templates in config:** In agent instructions or in condition expressions, use:
   - `{{ $steps.par_gen.outputs.gen_1.output }}` — output of parallel step `gen_1`
   - `{{ $steps.par_gen.outputs[0].output }}` — first output by `order`
   - Same for nested parallel: `{{ $steps.outer.outputs.inner.outputs.researcher.output }}` (or a flatter key like `inner.researcher` by convention).

So: **structured access by step ID** (and by index via `order`), both in injected context and in templates.

---

## Summary Table

| Topic                 | Decision                                                                 |
|-----------------------|--------------------------------------------------------------------------|
| Loop depth            | `max_loop_iterations` (default 100); per-cycle count per step ID        |
| Nested parallel       | Yes; parallel steps can contain parallel (or any) steps                  |
| Parallel output shape | Keyed map by step ID + `order` array; one blob to next step              |
| Retry                 | Per-step `retry.max_attempts` + optional backoff; workflow fails after  |
| Access parallel outs  | `$steps.<id>.outputs.<step_id>.output` and `$steps.<id>.outputs[n]`      |
| Parallel failure      | Any failure in a parallel block fails the whole workflow immediately    |

---

## How to run workflow via CLI

When your agent config defines a `workflow` section, use **exec** (non-TUI) to run the workflow:

```bash
# Run workflow from config (exec mode runs the workflow executor)
cagent exec ./agent-with-workflow.yaml

# With a prompt (passed as initial user message to the workflow)
cagent exec ./agent-with-workflow.yaml "Translate and publish this draft"

# With stdin
echo "Process these items" | cagent exec ./agent-with-workflow.yaml -
```

Workflow execution is **only** wired for **exec** mode. The `run` command (TUI) still uses single-agent mode even when the config has a workflow.

## Implementation Notes

- **Types:** `pkg/workflow` holds workflow and step types (Config, Step, StepContext, loop counter, condition evaluation). No dependency on runtime or session to avoid import cycles.
  - `StepContext` is concurrency-safe (`sync.RWMutex`) and exposes a `Snapshot()` method for serialization/debugging.
- **Executor:** `pkg/workflowrun` holds the executor: runs the workflow DAG (sequential/conditional/parallel), calls runtime `RunStream` per agent step, maintains step outputs and loop counters, evaluates conditions, and injects output context into sessions.
  - Use `workflowrun.NewLocalExecutor(runtime)` and `Executor.Run(ctx, cfg, sess, events)` which returns `(*workflow.StepContext, error)`.
  - After execution, the step context is printed to stderr as formatted JSON for debugging (`--- Step Context ---`).
  - **Context propagation:** `buildPriorContext()` collects all prior step outputs and injects them as a structured text block into the next step's user message.
  - **Parallel safety:** `runnerMu` serializes `SetCurrentAgent` + `RunStream` to prevent agent name races; sub-sessions skip SQLite persistence.
- **Session Store:** `SQLiteSessionStore` has a `sync.Mutex` protecting all write methods (`AddMessage`, `UpdateMessage`, `AddSession`, `UpdateSession`, etc.) to prevent concurrent write panics.
- **Config:** Workflow config lives in `pkg/config/latest` as `Config.Workflow` (type `*workflow.Config`). Validation in `validate.go` ensures agent names exist, step types are valid, and condition steps have a condition expression.
- **CLI:** When `Config.Workflow` is set, `cagent exec` uses the workflow executor and streams events to stdout; `cagent run` (TUI) still uses single-agent mode.

Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.
1 Letterman Drive
Suite D4700
San Francisco, CA, 94129

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.

Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.