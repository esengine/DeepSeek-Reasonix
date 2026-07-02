# AutoResearch Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the host-managed AutoResearch runtime from the design spec, including durable state, controller integration, desktop API, and frontend visibility.

**Architecture:** Add `internal/autoresearch` as the source of truth for task state under `.reasonix/autoresearch/<task-id>/`. Goal controller code creates, resumes, summarizes, heartbeats, and gates completion through that package. Desktop and frontend read compact status metadata for the active tab and fetch heavier details on demand.

**Tech Stack:** Go standard library, existing `internal/control` goal FSM, desktop Wails bindings, React/TypeScript frontend, existing Go and frontend test runners.

---

## Implementation Tasks

### Task 1: AutoResearch Store And Schema

**Files:**
- Create: `internal/autoresearch/task.go`
- Create: `internal/autoresearch/store.go`
- Create: `internal/autoresearch/schema.go`
- Create: `internal/autoresearch/store_test.go`

- [ ] Write failing tests that prove `CreateTask` creates `.reasonix/autoresearch/<task-id>/state` and `logs`, writes `task_spec.json`, `progress.json`, empty JSONL files, and validates required fields.
- [ ] Run: `go test ./internal/autoresearch -run 'TestCreateTask|TestValidateTask' -count=1`
- [ ] Implement types, task id generation, atomic JSON writes, JSONL append validation, `CreateTask`, `LoadTask`, `ValidateTask`.
- [ ] Run: `go test ./internal/autoresearch -count=1`

### Task 2: Findings, Directions, Summary, Readiness

**Files:**
- Create: `internal/autoresearch/summary.go`
- Create: `internal/autoresearch/readiness.go`
- Modify: `internal/autoresearch/store.go`
- Modify: `internal/autoresearch/store_test.go`

- [ ] Write failing tests for `AppendFinding`, `RecordDirection`, stale/pivot thresholds, `Summary`, and `Readiness`.
- [ ] Run: `go test ./internal/autoresearch -run 'TestAppendFinding|TestRecordDirection|TestReadiness|TestSummary' -count=1`
- [ ] Implement accepted evidence lookup, direction fingerprint tracking, stale count updates, pivot requirement, readiness blocking, and compact summary generation.
- [ ] Run: `go test ./internal/autoresearch -count=1`

### Task 3: Goal Controller Runtime Integration

**Files:**
- Modify: `internal/control/goal.go`
- Modify: `internal/control/input.go`
- Modify: `internal/control/controller.go`
- Modify: `internal/control/turn_orchestrator.go`
- Modify: `internal/control/goal_test.go`
- Modify: `internal/control/input_test.go`

- [ ] Write failing tests that `/goal --research` creates or resumes an AutoResearch task and stores `AutoResearchTaskID` in persisted goal state.
- [ ] Write failing tests that `Compose` injects a host-generated `<autoresearch-runtime>` summary only in the current user turn.
- [ ] Write failing tests that goal completion is intercepted when AutoResearch readiness fails.
- [ ] Run targeted control tests for the new cases.
- [ ] Implement store wiring, create/resume, heartbeat hooks, runtime summary injection, and readiness intercept.
- [ ] Run: `go test ./internal/control -count=1`

### Task 4: Desktop API Surface

**Files:**
- Modify: `desktop/app.go`
- Modify: `desktop/tabs.go`
- Modify: `desktop/app_test.go`
- Modify: `desktop/tab_profile_test.go`

- [ ] Write failing tests for `AutoResearchStatus`, `AutoResearchCurrent`, `AutoResearchFindings`, and compact tab metadata.
- [ ] Implement view structs and methods that call `internal/autoresearch` without parsing prompt text.
- [ ] Ensure findings are capped and loaded on demand.
- [ ] Run: `go test ./desktop -run 'AutoResearch|MetaReports' -count=1`

### Task 5: Frontend Status And Detail UI

**Files:**
- Modify: `desktop/frontend/src/lib/statusBarItems.ts`
- Modify: `desktop/frontend/src/components/StatusBar.tsx`
- Modify: `desktop/frontend/src/components/ContextPanel.tsx`
- Modify: `desktop/frontend/src/bridge.ts`
- Modify: `desktop/frontend/src/__tests__/statusbar-workspace.test.tsx`
- Add frontend tests for AutoResearch detail rendering.

- [ ] Write failing frontend tests for hidden chip, running/pivot/blocked labels, opening the detail panel, and rendering loaded findings.
- [ ] Implement compact status chip, tooltip, detail section, refresh hooks, and on-demand findings loading.
- [ ] Run: `pnpm --dir desktop/frontend test`

### Task 6: End-To-End Verification

**Files:**
- Update generated Wails bindings if required.
- No broad refactors.

- [ ] Run: `go test ./internal/autoresearch ./internal/control ./desktop -count=1`
- [ ] Run: `pnpm --dir desktop/frontend test`
- [ ] Run the desktop app through the project dev command when feasible and manually verify a research goal shows the chip and panel state.
- [ ] Audit each design-spec goal against implementation and record remaining gaps before marking the goal complete.
