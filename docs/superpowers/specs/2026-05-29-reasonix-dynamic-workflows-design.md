# Reasonix Built-in Dynamic Workflows Design

## Goal

Add a native `workflow` tool and Dynamic Workflows runtime to Reasonix.

The runtime path is:

```text
workflow script
  -> workflow runtime
  -> agent()
  -> Reasonix internal spawnSubagent()
  -> parallel()
  -> verifier
  -> synthesis
```

This is not a skill feature. Do not add `SKILL.md`, `.claude/skills`, `.agents/skills`, or
repo-level playbooks for workflow activation.

## Architecture

The implementation adds focused workflow modules:

- `src/workflow/parser.ts`: parses literal `export const meta = ...` and rejects unsafe script APIs.
- `src/workflow/runtime.ts`: runs scripts in a restricted sandbox and provides workflow globals.
- `src/workflow/agent-runner.ts`: adapts workflow `agent()` to internal `spawnSubagent()`.
- `src/tools/workflow.ts`: registers the built-in `workflow` tool.

The tool is registered from `src/code/setup.ts`, so code mode, desktop, and ACP surfaces that build
the code toolset receive the same native capability.

## Tool Guidance

The `workflow` tool description is the activation mechanism. It tells the model to use workflows
for repository-wide audits, large refactors, migrations, multi-module bug hunts, architecture and
security reviews, test coverage sweeps, multi-perspective verification, and explicit workflow or
parallel-agent requests.

It also tells the model not to use workflows for simple single-file edits, small bug fixes, direct
Q&A, formatting, or tasks that do not need parallel analysis.

## Runtime

Workflow scripts use raw JavaScript. The first statement must be literal metadata:

```js
export const meta = {
  name: "repo_audit",
  description: "Audit repository architecture and risks",
  phases: [{ title: "Inventory" }, { title: "Review" }],
};
```

Available globals:

- `agent(prompt, opts)`
- `parallel(thunks)`
- `pipeline(items, ...stages)`
- `phase(title)`
- `log(message)`
- `args`
- `cwd`
- `budget`

The sandbox does not expose shell, filesystem, network, imports, `require`, `process.env`, dynamic
import, or nondeterministic time/random APIs.

## Subagent Integration

`agent()` calls `ReasonixWorkflowAgentRunner`, which calls `spawnSubagent()` with:

- parent registry
- parent abort signal
- workflow-specific system prompt
- workflow-scoped `skillName`
- read-only allowed tool list by default

Child subagents do not inherit orchestration tools: `workflow`, `spawn_subagent`, or `submit_plan`.

## Modes and Limits

The tool supports:

- `run`
- `dry_run`
- `validate_only`

Default limits:

- concurrency: 3
- max agents: 8
- hard concurrency cap: 16
- hard max agents cap: 32

`tool_mode: "read_only"` is the default. `tool_mode: "full"` is treated as mutating and is blocked
by plan mode.

## Testing

Tests use mock runners and fake clients. They do not call the real DeepSeek API.

Coverage includes parser validation, sandbox helpers, dry-run, validate-only, max agents,
subagent adapter mapping, child registry exclusion, tool registration, public exports, prompt
budget, and code toolset lazy initialization.

