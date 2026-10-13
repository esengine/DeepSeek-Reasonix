# Cognee Settings Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Configure and diagnose Cognee project memory from Reasonix settings while keeping Cognee's token in the credential store and reusing the existing MCP runtime.

**Architecture:** Add a read-only readiness probe to the controller that distinguishes config, authorization, connection, and tool-schema states without calling `recall` or `remember`. Add a Cognee-specific desktop setup API that validates the endpoint, stores an optional token as a Reasonix credential, and uses existing MCP install/update and authorization methods. Put the setup card and form in Agent Skills settings and localize its states.

**Tech Stack:** Go desktop/controller, Wails-generated host contract, TypeScript/React, CSS, Reasonix credential/config and MCP lifecycle.

**Spec:** `docs/superpowers/specs/2026-10-13-cognee-settings-wizard-design.md`

## Global Constraints

- “Keep Cognee memory opt-in through the existing `cognee-memory` Skill toggle.”
- “Preserve normal task fail-open behavior when Cognee is unavailable.”
- “Keep MCP `forget` and other Cognee write tools inaccessible through this feature.”
- “Do not bundle, install, or manage a local Cognee backend/database.”
- “Keep existing general MCP management and provider-visible tool schemas unchanged.”
- “The MCP config stores only an environment-variable reference.”
- Setup readiness checks required tool schemas and does not call `recall` or `remember`.

## Review Focus

- **Unsafe or malformed endpoint:** reject non-HTTPS remote URLs, credentialed URLs, and non-loopback HTTP without saving config or a token. Verify through the configured endpoint validator and inspect its error path.
- **Empty or replaced token:** ensure a blank token does not erase an existing saved token and that raw token text never enters MCP config or status output. Verify the credential key and persisted config reference.
- **Existing project/plugin-owned `cognee`:** do not silently replace a config entry the current settings source cannot edit. Verify the setup action links to existing MCP details.
- **Authorization required:** surface the existing authorization action and do not bypass or duplicate MCP authorization. Verify the status mapping uses existing `ServerView`/auth actions.
- **Missing or incompatible tools:** report a not-ready state for absent or incompatible `recall`/`remember` schemas and do not issue either tool call from setup.

---

### Task 1: Readiness and secure desktop setup API

**Files:**
- Modify: `internal/control/cognee_memory.go`
- Modify: `internal/config/credentials.go`
- Create: `desktop/cognee_settings.go`
- Modify: `desktop/frontend/src/lib/bridge.ts`
- Modify: `desktop/frontend/src/lib/types.ts`
- Generate: `desktop/frontend/src/generated/desktopContract.generated.ts`, `desktop/frontend/src/generated/desktopContract.generated.json`

**Interfaces:**
- Produce `control.CogneeMemoryStatus` with JSON-safe fields `state`, `detail`, `skillEnabled`, `configured`, and `tokenConfigured`.
- Produce `(*Controller).CogneeMemoryStatus(context.Context) control.CogneeMemoryStatus`; states are `not_configured`, `disabled`, `authorization_required`, `connection_failed`, `tools_incompatible`, and `ready`.
- Have the desktop app reach the readiness method through a narrow local interface assertion on the current session controller, so `control.SessionAPI` does not gain a method that unrelated implementations must satisfy.
- Produce `(*App).CogneeMemorySettings() control.CogneeMemoryStatus` and `(*App).ConfigureCogneeMemory(url, token string) (control.CogneeMemoryStatus, error)`.
- `CogneeMemoryStatus` is read-only: use the current runtime connection and existing schema validators; never call `recall` or `remember`.
- `ConfigureCogneeMemory` validates HTTPS or loopback HTTP before any writes, creates or updates only the fixed `cognee` identity, saves nonempty bearer tokens through a revision-guarded Reasonix credential-store write under a fixed Cognee environment key, and persists only `${KEY}` in the MCP header. Empty token input preserves a stored token. Failed connection changes conditionally restore credentials and the prior MCP connection.

- [x] Inspect `Controller` runtime access, `MCPServerInput` update semantics, and host contract generation command before editing.
- [x] Refactor Cognee configuration/readiness checks so the desktop status returns the six defined states and safe, user-readable detail without changing recall/remember gating.
- [x] Add the desktop status and setup methods; delegate connect/update/auth to the existing MCP lifecycle and return the post-connection status.
- [x] Add typed frontend bridge declarations and regenerate the desktop host contract with `cd desktop && go run . -emit-contract frontend/src/generated`.
- [x] Run `cd desktop/frontend && pnpm typecheck`; expected: exit code 0.
- [x] Run `cd desktop && go build ./...`; expected: exit code 0.

### Task 2: Cognee status card and setup flow

**Files:**
- Modify: `desktop/frontend/src/components/CapabilitiesPanel.tsx`
- Modify: `desktop/frontend/src/styles.css`
- Modify: `desktop/frontend/src/locales/en.ts`
- Modify: `desktop/frontend/src/locales/zh.ts`
- Modify: `desktop/frontend/src/locales/zh-TW.ts`

**Interfaces:**
- Consume `app.CogneeMemorySettings()` and `app.ConfigureCogneeMemory(url, token)` from Task 1.
- Consume existing `app.AuthenticateMCPServer("cognee")`, `app.AuthorizeAndConnectMCPServer("cognee")`, `app.MCPServers()`, and the `cognee-memory` Skill toggle.
- Keep setup UI local to Agent Skills settings; do not add a new settings route.

- [x] Add a Cognee project-memory card to Agent Skills settings with the six status labels, safe detail, Skill toggle, and configure/retry actions.
- [x] Add a compact setup form for the MCP URL and optional password-style bearer token; validate URL locally for immediate feedback and rely on the backend as the authoritative validator.
- [x] For an existing entry that cannot be edited from this surface, show its current status and link to its MCP detail instead of overwriting it.
- [x] Route authorization-required states through the existing MCP authorization actions; show connection and schema failures with their specific safe detail.
- [x] Add English, Simplified Chinese, and Traditional Chinese labels; preserve existing button/focus styles and responsive card layout.
- [x] Run `cd desktop/frontend && pnpm typecheck`; expected: exit code 0.

### Task 3: Documentation and full build verification

**Files:**
- Modify: `docs/COGNEE_MCP.zh-CN.md`
- Modify: `docs/superpowers/specs/2026-10-13-cognee-settings-wizard-design.md` only if implementation discovers a real spec discrepancy.

**Interfaces:**
- Document the Agent Skills setup card as the preferred configuration path and keep the manual TOML example for advanced use.
- Do not document or expose the Cognee bearer token in plain config.

- [x] Update Cognee setup documentation with the in-app flow, HTTPS/loopback rule, credential storage behavior, and readiness statuses.
- [x] Run frontend typecheck, ESLint, CSS syntax validation, and the production Vite build. The package `pnpm build` wrapper was not run because it launches test files, which the active developer instruction prohibits.
- [x] Run `git diff --check` and inspect the final diff; verify the three pre-existing unrelated local edits remain unstaged.
- [ ] Commit only the planned implementation files and push the approved feature commit to `origin main-v2`.

## Verification boundaries

Per the active developer instruction, do not add or run test suites unless the user asks for tests. Use the specified type checks, generated host contract, production build, and manual diff/status review.
