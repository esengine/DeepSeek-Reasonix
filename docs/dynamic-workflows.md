# Dynamic Workflows

Reasonix includes a native `workflow` tool for tasks that benefit from fan-out/fan-in
analysis across internal subagents.

Use workflows for repository-wide audits, large refactors, migrations, multi-module bug hunts,
architecture reviews, security reviews, test coverage sweeps, multi-perspective verification, or
when the user explicitly asks for workflows, dynamic workflows, parallel agents, subagents, or
fan-out analysis.

Do not use workflows for simple single-file edits, small bug fixes, direct Q&A, formatting tasks,
or when the user asks not to spawn subagents.

## Script Shape

Workflow scripts are raw JavaScript strings. The first statement must export literal metadata:

```js
export const meta = {
  name: "repo_audit",
  description: "Audit repository architecture and risks",
  phases: [{ title: "Inventory" }, { title: "Review" }, { title: "Synthesis" }],
};
```

After the metadata export, the script may use these globals:

- `agent(prompt, opts)`
- `parallel(thunks)`
- `pipeline(items, ...stages)`
- `phase(title)`
- `log(message)`
- `args`
- `cwd`
- `budget`

Example:

```js
export const meta = {
  name: "repo_audit",
  description: "Audit repository architecture and risks",
  phases: [{ title: "Inventory" }, { title: "Review" }, { title: "Synthesis" }],
};

phase("Inventory");
const inventory = await agent("Map the repository structure and key modules.", {
  label: "repo inventory",
  type: "explore",
});

phase("Review");
const reviews = await parallel([
  () => agent("Review architecture risks:\n" + inventory, { label: "architecture review" }),
  () => agent("Review security-sensitive paths:\n" + inventory, { label: "security review" }),
]);

phase("Synthesis");
const synthesis = await agent("Synthesize these findings:\n" + JSON.stringify(reviews), {
  label: "final synthesis",
  type: "synthesis",
});

return { inventory, reviews, synthesis };
```

`parallel()` receives functions, not promises. Use `() => agent(...)`, not `agent(...)`.

## Execution Modes

- `run`: execute the workflow and spawn internal Reasonix subagents.
- `dry_run`: execute the script with stub agents and return the planned agent calls.
- `validate_only`: parse metadata and safety-check the script without executing the body.

## Safety Model

Workflow scripts run in a restricted `node:vm` context. They do not receive `require`, imports,
filesystem APIs, network APIs, `process.env`, `child_process`, `Date.now()`, `new Date()`, or
`Math.random()`.

By default, workflow subagents are read-only and receive only registered safe read/search tools.
`tool_mode: "full"` is reserved for workflows that intentionally need normal inherited tools; it
still goes through Reasonix's existing permissions and is unavailable in plan mode.

`agent()` uses Reasonix's internal `spawnSubagent()` implementation. It does not start an external
`reasonix run` process.

## Limits and Abort

The runtime defaults to conservative fan-out:

- concurrency: 3
- max agents: 8
- hard concurrency cap: 16
- hard max agents cap: 32

The parent turn's abort signal is forwarded into active subagents. Queued workflow tasks stop before
starting if the workflow is aborted.

