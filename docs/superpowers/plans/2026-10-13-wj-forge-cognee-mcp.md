# WJ Forge Cognee MCP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` after the user approves this plan and selects native execution. Work one task at a time and keep the changes reviewable.

**Goal:** Add WJ Forge's opt-in Cognee project-memory lifecycle to Reasonix through its existing MCP connection and evidence boundaries.

**Architecture:** Keep Cognee on the existing configured MCP server named `cognee`. Restrict model-routed Cognee tool calls in the `use_capability` resolver, and add a controller-owned managed call path for only schema-validated `recall` and `remember`. Carry recall as transient untrusted turn context, and allow one durable remember attempt only after a completed turn has completed todos and usable verification evidence.

**Tech Stack:** Go, Reasonix `plugin.Host`, `agent.MCPCapabilityRuntime` / `UseCapabilityTool`, controller turn orchestration, event and evidence ledgers, existing credential store.

**Spec:** `docs/superpowers/specs/2026-10-13-wj-forge-skills-cognee-design.md`

## Global Constraints

- Cognee lifecycle requires the `cognee-memory` Skill enabled and the configured MCP server authorized and connected; missing Cognee must not block normal work.
- Recall is at most once per submitted task; its bounded, redacted result is untrusted context and never authorization.
- Remember requires completed task status, all task items completed, and verified work evidence; persist an at-most-once attempt before sending.
- Only internal lifecycle code may call Cognee `recall` and controlled `remember`; the model cannot call Cognee `forget` or any Cognee write tool.
- Use the existing credential store or MCP secret reference; never expose secrets in ordinary config, prompts, events, logs, or memory payloads.
- Remote Cognee endpoints require HTTPS; HTTP is allowed only for loopback.
- Keep provider-visible tool names and schemas unchanged; use the existing MCP transport and `use_capability` proxy.
- Do not add or run tests unless the user explicitly asks for tests or implementation verification.

## Review Focus

- A tool-list refresh or same-name server replacement must not let a managed call execute against a stale MCP identity or schema.
- A server with `recall` but no dataset selector, or with a mismatched `remember` schema, must fail closed for memory and keep the user task moving.
- A disconnected/unauthorized Cognee server must never be auto-connected by recall/remember.
- A timeout after dispatch must leave the durable attempt marker set so a follow-up does not duplicate a remote write.
- Retrieved prompt injection, secrets, absolute workspace paths, and oversized responses must be removed or bounded before session context, events, and logs.

---

### Task 1: Add an identity-bound managed MCP call primitive

**Files:**
- Modify: `internal/plugin/plugin.go`
- Modify: `internal/control/mcp.go`

**Produces:** A host-side API for trusted controller integrations to call one named raw tool on an already connected server only when the supplied `plugin.Spec` still matches the live client and is authorized.

- [ ] Add `(*plugin.Host).CallToolForSpec(ctx context.Context, spec plugin.Spec, rawToolName string, args json.RawMessage) (string, error)`. Resolve tools through `ToolsForSpec`, match exact `MCPRawToolName`, require `MCPServerAuthorized`, and execute the matched tool; never connect a server as a side effect.
- [ ] Add a `mcpManager` forwarding method that returns an unavailable error when no host exists; do not read credentials or rebuild a spec from raw config in the forwarding method.
- [ ] Format Go files and run `go build ./...`; inspect the API path to confirm it cannot reach a same-name but different runtime spec.

### Task 2: Implement the Cognee lifecycle service

**Files:**
- Create: `internal/control/cognee_memory.go`
- Create: `internal/control/cognee_memory_store.go`
- Modify: `internal/control/controller.go` (owned service/options and safe status accessor)
- Modify: `internal/plugin/plugin.go` only if Task 1's API needs narrow hardening

**Consumes:** Task 1's identity-bound managed call API; Task 1 of `2026-10-13-wj-forge-skills.md` provides the stable `cognee-memory` Skill metadata.

**Produces:** A Cognee service that offers only `Recall(ctx, workspace, request)` and `Remember(ctx, workspace, evidenceSummary)`; all other tool names are rejected internally.

- [ ] Add a compiled server-name constant `cognee` and validate its endpoint: HTTPS for remote hosts, HTTP only for loopback. Reuse `config.PluginEntry` expansion and the existing credential store; do not store a token in Cognee-specific plaintext state.
- [ ] Derive a stable dataset name from the canonical workspace path using SHA-256; send only the dataset identifier, not the absolute path.
- [ ] For recall, find the live authorized `recall` tool and require a string `query` plus either string `dataset_name` or string-array `datasets`; call once with a redacted, bounded query. Redact and cap the response before returning it as untrusted content.
- [ ] For remember, require string `data` and `dataset_name`, build a short payload only from validated decisions and verified evidence, strip credential labels/opaque secrets and rejected speculation, then persist an attempt marker keyed by session and submitted task before dispatch.
- [ ] Store only safe status values and bounded/redacted failure details. If the remote result is uncertain, keep the marker and do not retry automatically; if no eligible evidence exists, do not dispatch.
- [ ] Format Go files and run `go build ./...`; manually check both schema paths and the persisted pre-dispatch attempt order.

### Task 3: Enforce Cognee's model-call restrictions and transient recall context

**Files:**
- Modify: `internal/agent/usecapability.go`
- Modify: `internal/agent/usecapability_list.go`
- Modify: `internal/agent/usecapability_inspect.go`
- Modify: `internal/agent/agent.go`
- Modify: `internal/agent/coordinator.go`

**Consumes:** Task 2's `CogneeMemory.Recall` and safe status result.

**Produces:** Model capability discovery and execution cannot call any Cognee tool directly; a one-turn context value can be supplied to both planner and executor without persisting retrieved text in the transcript.

- [ ] Filter `mcp-tool:cognee/<tool>` from model-callable catalog/list/inspect results and reject the same IDs again in `UseCapabilityTool.resolveCall`, including cached, live, and lazy tool paths. Keep ordinary `mcp-server:cognee` status/connection management unchanged.
- [ ] Add `agent.WithTransientTurnContext(ctx context.Context, label, content string) context.Context`; consume it in planner and executor request construction as a bounded, explicitly untrusted block, without appending it to persisted conversation messages.
- [ ] Ensure `remember` and every other Cognee write tool are blocked at the proxy even if the server schema refreshes mid-session or a tool ID is supplied directly.
- [ ] Format Go files and run `go build ./...`; inspect serialized provider tool definitions before and after the changes for exact equality.

### Task 4: Hook recall/remember into the controller task lifecycle

**Files:**
- Modify: `internal/control/turn_orchestrator.go`
- Modify: `internal/control/controller.go`
- Modify: `internal/agent/agent.go` (export a compact completion snapshot)
- Modify: `internal/event/notice_codes.go` (Cognee lifecycle notice code)

**Consumes:** Tasks 2–3's service and transient context API.

**Produces:** Recall occurs before planning/execution once for the visible submitted task; remember runs only after the whole orchestrated task returns successfully and completion/evidence gates pass.

- [ ] At the visible-task boundary in `runOrchestratedTurn`, require `cognee-memory` enabled and the configured `cognee` server already authorized and connected; otherwise skip without connecting. Pass the latest raw user request to recall and bind any result with `agent.WithTransientTurnContext` before `runModelTurn`.
- [ ] Add `Agent.CompletionSnapshot()` returning only `TodosComplete`, `HasVerifiedEvidence`, and compact evidence categories derived from recognized successful verification/review receipts; no raw receipt args or transcript. At the end of the full orchestrated flow, after approved-plan execution, require successful completion and both snapshot gates.
- [ ] Call Remember at most once for the submitted task; persist the marker before network dispatch. Do not remember after cancellation, provider error, incomplete todos, absent verification, or uncertain prior attempt.
- [ ] Emit one redacted `event.Notice` using `event.NoticeCodeCogneeMemory` for skipped/retrieved/saved/failed outcomes without embedding recalled text or credentials.
- [ ] Format Go files and run `go build ./...`; manually walk the plan-only, approved-plan, cancellation, no-evidence, disconnected-server, and uncertain-timeout paths.

### Task 5: Document setup and runtime behavior

**Files:**
- Create: `docs/COGNEE_MCP.zh-CN.md`
- Modify: `docs/TOOL_CONTRACT.zh-CN.md` only if a note is needed to clarify the unchanged proxy contract

**Consumes:** Tasks 1–4's public setup and behavior.

**Produces:** User-facing setup steps and clear explanations of opt-in, endpoint/TLS rules, transient untrusted recall, automatic evidence-gated remember, and failure behavior.

- [ ] Document configuring an MCP server named `cognee`, providing credentials through Reasonix's credential/secret mechanism, and enabling the `cognee-memory` Skill.
- [ ] Explain the exact recall and remember gates, that `forget` is unavailable, and that Cognee failures do not stop normal work.
- [ ] Verify config examples use the existing Reasonix MCP syntax and introduce no new provider-visible tool names.
