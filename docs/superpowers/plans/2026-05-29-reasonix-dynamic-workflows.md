# Reasonix Dynamic Workflows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` or `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** Build a native Reasonix `workflow` tool and runtime that executes deterministic JavaScript
workflow scripts and maps `agent()` calls to internal `spawnSubagent()`.

**Architecture:** Add focused `src/workflow/*` modules for parser, runtime, and subagent adapter.
Register `workflow` as a normal `ToolRegistry` tool from `src/code/setup.ts`; keep subagent
execution inside Reasonix by calling `spawnSubagent()` directly.

**Tech Stack:** TypeScript ESM, Vitest, `ToolRegistry`, `CacheFirstLoop`, `spawnSubagent()`,
`node:vm`, `acorn`.

---

## Tasks

- [x] Add parser tests and implement `src/workflow/parser.ts`.
- [x] Add runtime helper tests and implement `src/workflow/runtime.ts`.
- [x] Add `ReasonixWorkflowAgentRunner` and route workflow agents through `spawnSubagent()`.
- [x] Exclude `workflow` from child subagent registries.
- [x] Add built-in `workflow` tool registration in `src/tools/workflow.ts`.
- [x] Register workflow in `src/code/setup.ts`.
- [x] Export workflow public APIs from `src/index.ts`.
- [x] Update public API snapshot.
- [x] Verify prompt budget.
- [x] Add user docs and example.

## Verification

Focused verification:

```bash
npm run test -- tests/workflow-parser.test.ts tests/workflow-runtime.test.ts tests/workflow-agent-runner.test.ts tests/workflow-tool.test.ts tests/subagent.test.ts tests/code-setup-lazy-subagent.test.ts tests/public-api.test.ts tests/prompt-budget.test.ts
npm run typecheck
npx biome check src/workflow src/tools/workflow.ts src/code/setup.ts src/index.ts src/tools/subagent.ts tests/workflow-parser.test.ts tests/workflow-runtime.test.ts tests/workflow-agent-runner.test.ts tests/workflow-tool.test.ts tests/public-api.test.ts tests/subagent.test.ts
```

Final verification:

```bash
npm run lint
npm run typecheck
npm run test
```

