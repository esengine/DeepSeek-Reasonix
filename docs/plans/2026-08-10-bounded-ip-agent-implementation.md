# Bounded IP Agent Implementation Plan

> **For Codex:** Execute this plan with test-first changes, verify each task before advancing, and keep the Agent unable to perform generic or irreversible actions.

**Goal:** Add a Reasonix-derived, permission-bounded natural-language task agent for document IP intelligence and Wiki work, with deterministic evidence-grounded deliverables and a complete real-browser E2E path.

**Architecture:** The same-origin Node gateway owns a persistent domain orchestrator. DeepSeek proposes a strictly validated plan and synthesizes a fixed result contract; server-owned read-only tools query the existing permission-filtered publication, Wiki, evidence and graph adapters. SQLite stores tasks and step receipts. The Astro UI presents a task workbench, not a general chat or document editor.

**Tech Stack:** Node.js ESM, Astro, SQLite (`better-sqlite3`), DeepSeek chat completions, Node test runner, Playwright E2E.

---

### Task 1: Lock the policy and plan contracts

**Files:**
- Create: `site/server/agent-policy.mjs`
- Create: `site/server/agent-policy.test.mjs`
- Create: `site/server/agent-contract.mjs`
- Create: `site/server/agent-contract.test.mjs`

**Output:** Request policy, supported intents, tool allowlist, plan validation, result normalization and evidence-grounding gate.

**Test:** Start with failing tests for allowed IP/Wiki requests, coding/network/destructive/admin rejections, six-step/twelve-call limits, unknown tools, cross-workspace-like arguments and unsupported evidence IDs. Run `node --test server/agent-policy.test.mjs server/agent-contract.test.mjs`.

### Task 2: Add persistent task and receipt storage

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/server/backup-service.mjs`
- Modify: `site/server/backup-service.test.mjs`

**Output:** Workspace-scoped `agent_tasks` and `agent_task_events`, creator-only lookup/listing, append-only events, interrupted-task recovery and backup schema verification.

**Test:** Add persistence, workspace isolation, creator isolation, event ordering and restart recovery tests. Run `node --test server/platform-store.test.mjs server/backup-service.test.mjs`.

### Task 3: Implement DeepSeek planner/synthesizer and domain tools

**Files:**
- Create: `site/server/agent-model-client.mjs`
- Create: `site/server/agent-model-client.test.mjs`
- Create: `site/server/agent-tools.mjs`
- Create: `site/server/agent-tools.test.mjs`
- Create: `site/server/agent-service.mjs`
- Create: `site/server/agent-service.test.mjs`

**Output:** Two-stage model client, permission-safe domain tool registry, bounded async lifecycle, step receipts, cancellation, fail-closed errors and audit callbacks.

**Test:** Use fake model clients and real publication fixtures. Verify viewer/editor visibility, no Wiki mutation, plan rejection before execution, missing-evidence downgrade, timeout/failure, cancellation and restart behavior. Run the six focused test files.

### Task 4: Expose authenticated Agent APIs

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`
- Modify: `site/server/config.mjs`
- Modify: `site/server/config.test.mjs`

**Output:** `POST/GET /api/agent/tasks`, `GET /api/agent/tasks/:id`, `POST /api/agent/tasks/:id/cancel`, per-user rate limiting, RBAC, origin checks, audit events and health disclosure.

**Test:** Verify authentication, creator/workspace isolation, role changes, input limits, rate limits, blocked requests, safe errors and task lifecycle through HTTP.

### Task 5: Build the intelifar IP task workbench

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`
- Create: `site/src/scripts/agent-workbench.mjs`
- Create: `site/src/scripts/agent-workbench.test.mjs`

**Output:** New navigation module, template intents, natural-language task form, permanent boundary indicators, six-step ledger, structured result package, evidence links, task history, empty/error/blocked/cancelled states and responsive layout.

**Test:** DOM contract tests plus unit tests for safe rendering and state transitions. Run `npm test` and `npm run build`.

### Task 6: Run offline and real-provider E2E

**Files:**
- Create: `site/e2e/agent-workbench.e2e.mjs`
- Create: `site/e2e/agent-real.e2e.mjs`
- Modify: `site/package.json`
- Create/Update: `artifacts/ip-agent/*`

**Output:** Authenticated browser flow for a grounded impact task, editor Wiki draft, viewer visibility restriction, prohibited coding/deployment task, task history, cancellation/error state and mobile layout. Optional real suite reads existing runtime credentials through `loadRuntimeConfig` without exposing them and runs one bounded DeepSeek task over published real corpus assets.

**Test:** `npm run test:e2e:agent`, `npm run test:e2e:agent:real`, full `npm test`, `npm run build`, existing E2E suites and `npm run security:scan`. Save reviewed desktop/mobile screenshots and a final delivery-structure screenshot.

### Task 7: Document and hand off

**Files:**
- Modify: `docs/architecture/intelifar-ip-wiki.md`
- Modify: `docs/INTELIFAR-USER-GUIDE.zh-CN.md`
- Modify: `INTELIFAR-DELIVERY.md`
- Add: `artifacts/ip-agent/README.md`

**Output:** Chinese external-user instructions with screenshots, capability boundary, seven supported scenarios, result interpretation, evidence behavior and troubleshooting; architecture and delivery manifest updated.

**Test:** Documentation contract tests, link/path checks, final clean `git status`, and an intentional feature-branch commit history.
