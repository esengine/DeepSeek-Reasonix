# Cognee Memory Enrichment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Use the VISION-LIAO Cognee MCP design to retrieve authorized guidance and submit useful task outcomes as private/global proposals for desktop review.

**Architecture:** Keep lifecycle ownership in `turn_orchestrator.go`; distinguish absent TODOs from incomplete TODOs. Retrieve context through `get_guidance` (falling back to `search_memory`) without caller-supplied identity or scope. Build a sanitized task record once, then submit independent `propose_memory` requests to connector-private and global scopes, with separate idempotency state and user-visible pending-review outcomes. Do not call or fall back to the removed `remember`/`recall` APIs.

**Tech Stack:** Go, existing MCP tool schema/call abstractions, Reasonix evidence ledger, Go tests.

**Spec:** `docs/superpowers/specs/2026-10-15-cognee-memory-enrichment-design.md`

## Global Constraints

- Keep the MCP endpoint, Skill enablement, authorization, endpoint validation, and model-visible tool restrictions unchanged. Restrict the managed host allowlist to `get_guidance`, `search_memory`, and `propose_memory`.
- Never save failed, cancelled, incomplete-TODO, empty, or trivial turns.
- Never upload raw transcripts, credentials, or absolute workspace paths.
- Private and global proposal calls must fail independently and use separate one-attempt-per-task markers.
- Proposals never directly apply memory; notices must not claim approval or persistence. Do not send dataset names, connector IDs, or session IDs; VISION-LIAO derives identity and scope from the connector credential.
- Do not claim source-file/dataset management or graph/session browsing support in this MCP-only phase.

## Review Focus

- No TODO was written: a useful successful answer may still be proposed in the connector-private scope; pin with a lifecycle eligibility test.
- A TODO exists but is incomplete: neither proposal is submitted; pin with a test covering both scopes.
- VISION-LIAO schema must expose a supported read tool and proposal fields for content, category, scope, and source; missing fields or unsupported private/global scope fail closed.
- One destination fails or times out: the other destination still completes, and retry state remains independent; pin with MCP call-result tests.
- Task request or answer contains credentials and absolute paths: persisted payload is redacted and bounded; pin with summary construction tests.

---

### Task 1: Expose TODO presence and durable task evidence

**Files:**
- Modify: `internal/agent/agent.go` (`CompletionSnapshot` and `CompletionSnapshot()`)
- Create: `internal/agent/completion_snapshot_test.go`

**Interfaces:**
- Produces: `CompletionSnapshot{TodosWritten bool, TodosComplete bool, HasVerifiedEvidence bool, HasObservedMutation bool, EvidenceCategories []string}`. `TodosComplete` remains false when no TODO was written; callers use `TodosWritten` to distinguish that case.

- [ ] **Step 1: Write failing tests** for no TODO, all-complete TODOs, incomplete TODOs, successful verification/review evidence, and a successful observed mutation.
- [ ] **Step 2: Run the focused agent test** and confirm the new snapshot fields/assertions fail.
- [ ] **Step 3: Implement the snapshot fields** using `TodoStateSnapshot()` and the existing evidence ledger methods; do not expose transcript content or tool receipts.
- [ ] **Step 4: Run the focused agent test** and confirm all snapshot cases pass.

### Task 2: Build sanitized private and global proposals with schema-aware arguments

**Files:**
- Modify: `internal/control/cognee_memory.go`
- Modify: `internal/control/cognee_memory_test.go`

**Interfaces:**
- Consumes: Task 1 `agent.CompletionSnapshot` fields at the lifecycle call site.
- Produces: schema-aware `get_guidance` / `search_memory` reads and `propose_memory` payloads; `buildCogneeTaskRecord(task, outcome, workspace string, categories []string) string`; separate private/global proposal helpers.

- [ ] **Step 1: Write failing tests** for new read/proposal schema discovery; rejecting legacy schemas and unsupported scopes; redaction/bounds; distinct private/global arguments; separate attempt keys; and independent proposal failures.
- [ ] **Step 2: Run focused Cognee tests** and confirm the new schema, payload, and destination assertions fail.
- [ ] **Step 3: Implement schema-aware tool selection and record construction.** Prefer `get_guidance`, fall back to `search_memory`, and send only the task/query field declared by its schema. Record only the sanitized task intent, concise visible outcome, and evidence categories; keep the payload bounded and reject trivial outcomes.
- [ ] **Step 4: Implement independent proposals and markers.** Submit the same sanitized outcome separately as category `experience` to `private` and, with durable evidence, `global`. Send no dataset, connector, or session selectors. Persist distinct attempt markers before each MCP call; treat call errors as uncertain without automatic retry; report pending review, never applied memory.
- [ ] **Step 5: Run focused Cognee tests** and confirm both compatible and legacy schemas behave as specified.

### Task 3: Retrieve authorized VISION-LIAO guidance

**Files:**
- Modify: `internal/control/cognee_memory.go`
- Modify: `internal/control/cognee_memory_test.go`

**Interfaces:**
- Consumes: schema-selected task/query field from Task 2.
- Produces: safe read arguments from the declared task/query field only; identity and memory scope remain server-derived from connector credentials.

- [ ] **Step 1: Write failing tests** for guidance preference, search fallback, and rejection of legacy recall-only servers.
- [ ] **Step 2: Run the focused Cognee tests** and confirm the new guidance args and legacy-tool rejection.
- [ ] **Step 3: Add new read-tool selection** without weakening query redaction or untrusted-context treatment.
- [ ] **Step 4: Run focused Cognee tests** and confirm both current and legacy argument sets.

### Task 4: Apply eligibility rules at successful visible-turn completion and document them

**Files:**
- Modify: `internal/control/turn_orchestrator.go`
- Modify: `internal/control/turn_orchestrator_test.go`
- Modify: `docs/COGNEE_MCP.zh-CN.md`

**Interfaces:**
- Consumes: Task 1 completion snapshot and Task 2 `buildCogneeTaskRecord` plus private/global proposal helpers.
- Produces: one lifecycle decision after a successful, non-synthetic visible task. Private proposal is eligible for any nontrivial sanitized outcome when no TODO was written or all written TODOs are complete. Global proposal is eligible when the task has successful verification/review evidence or an observed project mutation, and any written TODOs are complete. Failed, cancelled, synthetic, empty, and trivial turns skip both. Private and global proposal outcomes are reported separately.

- [ ] **Step 1: Write failing lifecycle tests** for no-TODO private proposal; completed-TODO private proposal; incomplete-TODO skip; evidence-backed global proposal; observed-mutation global proposal; no-evidence private-only proposal; and failed/cancelled skip.
- [ ] **Step 2: Run the focused lifecycle tests** and confirm the current strict gate fails the new expectations.
- [ ] **Step 3: Replace the all-or-nothing gate** with independent eligibility and proposal notices, keeping timeouts bounded and the memory service fail-open.
- [ ] **Step 4: Update VISION-LIAO documentation** to describe guidance/search, proposal review, eligibility, privacy bounds, independent proposal status, and the absence of a session-record API.
- [ ] **Step 5: Run focused Cognee and orchestrator tests** and inspect the final diff for scope and privacy regressions.
