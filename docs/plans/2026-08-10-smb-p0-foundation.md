# intelifar SMB P0 Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a single-node small-business foundation with real sessions, workspace isolation, durable jobs, SQLite publications, and editable versioned Wiki content.

**Architecture:** Add a SQLite-backed platform store behind server-side interfaces and keep the existing MinerU → DeepSeek orchestration unchanged. The same-origin gateway authenticates each API request, authorizes by role, scopes every repository call to a workspace, and exposes Wiki editing/version history without sending credentials to the browser.

**Tech Stack:** Node.js 22, Astro 7, better-sqlite3, Node crypto/scrypt, Node test runner, Playwright browser E2E.

---

### Task 1: SQLite platform store

**Files:**
- Create: `site/server/platform-store.mjs`
- Create: `site/server/platform-store.test.mjs`
- Modify: `site/package.json`
- Modify: `site/package-lock.json`

**Steps:**
1. Write failing tests for migrations, workspace bootstrap, unique publication, job persistence, Wiki versions, audit hash chaining, and workspace isolation.
2. Run `node --test server/platform-store.test.mjs` and confirm the module is missing.
3. Add `better-sqlite3` and implement migrations plus parameterized repository methods.
4. Run the store tests and confirm they pass.

### Task 2: Local account and session service

**Files:**
- Create: `site/server/auth-service.mjs`
- Create: `site/server/auth-service.test.mjs`

**Steps:**
1. Write failing tests for scrypt password verification, opaque session cookies, expiry, disabled users, and role checks.
2. Implement bootstrap owner creation, login, logout, session lookup, and role authorization.
3. Verify passwords and raw session tokens never enter database responses or logs.
4. Run the auth tests.

### Task 3: Durable analysis orchestration

**Files:**
- Modify: `site/server/analysis-service.mjs`
- Modify: `site/server/analysis-service.test.mjs`
- Modify: `site/server/real-analysis-server.mjs`

**Steps:**
1. Add failing tests that submit a workspace-scoped job, persist state transitions, and recover an interrupted job after restart.
2. Add job-store and upload-store adapters while retaining the in-memory adapter for isolated unit tests.
3. Add authenticated retry and list-job endpoints.
4. Verify successful jobs clean temporary uploads and failed/interrupted jobs remain retryable.

### Task 4: Authenticated and workspace-scoped API

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`
- Replace behavior through: `site/server/publication-registry.mjs`

**Steps:**
1. Add failing tests for login, logout, 401, 403, tenant isolation, and idempotent workspace publication.
2. Add `/api/auth/login`, `/api/auth/logout`, and `/api/session`.
3. Require Viewer for reads and Editor for upload, retry, publish, and Wiki edits.
4. Scope all repository operations by authenticated workspace.
5. Run API tests.

### Task 5: Editable versioned Wiki

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/enterprise-contract.test.mjs`

**Steps:**
1. Add contract tests for login, edit, save, version history, conflict status, and logout hooks.
2. Add the intelifar-styled login gate, current workspace identity, Wiki edit dialog, and version-history drawer.
3. Load session before protected data; handle 401 without leaking previous workspace content.
4. Submit edits with a base version and render the returned version.
5. Verify keyboard access, focus behavior, responsive layout, and reduced-motion compatibility.

### Task 6: Full verification and delivery evidence

**Files:**
- Modify: `site/e2e/platform.e2e.mjs`
- Create: `site/e2e/smb-auth-wiki.e2e.mjs`
- Modify: `INTELIFAR-DELIVERY.md`
- Create: `artifacts/smb-p0-review/*`

**Steps:**
1. Run `npm test` and fix regressions.
2. Run `npm run build`.
3. Run offline browser E2E plus authenticated Wiki E2E.
4. Run `npm run security:scan` and inspect logs/artifacts for credentials.
5. Capture desktop and mobile screenshots for login, workspace identity, Wiki editing, version history, and interrupted-job recovery.
6. Update delivery boundaries: SQLite is the single-node SMB adapter; PostgreSQL/object storage remain the multi-instance production adapter.
