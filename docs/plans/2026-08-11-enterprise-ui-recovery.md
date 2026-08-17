# Enterprise UI Recovery Implementation Plan

> **For Codex:** Execute this plan test-first and keep each change independently verifiable.

**Goal:** Restore the 4388 real UI to a persistent, auditable workspace and remove stale/demo data ambiguity.

**Architecture:** Keep the existing Astro UI and Node gateway. Make the CLI default to SQLite, seed a non-authenticatable loopback owner, migrate legacy publications transactionally, expose workspace-scoped dashboard/audit read models, and make evidence rendering fail closed.

**Tech Stack:** Node.js ESM, better-sqlite3, Astro, node:test, Playwright-compatible E2E.

---

### Task 1: Persistent loopback startup

**Files:** `site/server/real-analysis-server.mjs`, `site/server/real-analysis-server.test.mjs`

1. Add a failing test with a temporary database proving loopback mode lists one owner, reports SQLite/Agent/backup support, and appends audit events.
2. Seed the loopback workspace owner idempotently and record loopback audit events.
3. Make the command-line entry always pass the configured/default SQLite path.
4. Run the focused server test.

### Task 2: Legacy publication migration

**Files:** `site/server/publication-registry.mjs`, `site/server/publication-registry.test.mjs`, `site/server/real-analysis-server.mjs`

1. Add a failing test that imports an atomic `registry.json` into an empty platform store and remains idempotent.
2. Implement `migrateLegacyPublications(workspaceId)` with validation and duplicate suppression.
3. Invoke it before the server starts serving requests.
4. Run registry and platform-store tests.

### Task 3: Workspace dashboard and audit APIs

**Files:** `site/server/platform-store.mjs`, `site/server/platform-store.test.mjs`, `site/server/real-analysis-server.mjs`, `site/server/real-analysis-server.test.mjs`

1. Add a public, sanitized `listAudit` method and tests.
2. Add authenticated/workspace-scoped `/api/audit` and `/api/dashboard` routes.
3. Add an allowlisted `/api/audit/events` write route for UI evidence views/exports; reject arbitrary action names and object identifiers.
4. Test role access, input bounds, workspace isolation and chain validity.

### Task 4: Truthful UI and fail-closed evidence

**Files:** `site/src/pages/index.astro`, `site/src/scripts/ip-platform.mjs`, frontend contract tests.

1. Add stable IDs/data attributes for real dashboard and audit KPIs.
2. Fetch and render the real dashboard/audit models after session initialization.
3. Replace the redaction stale-drawer fallback with a dedicated contextual evidence object and server audit event.
4. Make unknown provenance buttons close stale context and show an error.
5. Export the currently rendered real audit ledger with spreadsheet-formula neutralization.
6. Reset graph camera at the mobile breakpoint and add an honest zero-edge empty state.

### Task 5: Verification and real UI acceptance

**Files:** `site/e2e/module-tour.e2e.mjs`, new or updated recovery E2E, `artifacts/`

1. Build the site and run all Node unit/integration tests.
2. Run module, SMB operations, collaboration, bounded Agent and real UI E2E suites.
3. Restart port 4388 with the migrated SQLite database.
4. Verify through the visible UI: SQLite health, one member, valid audit chain, backup creation/verification, contextual redaction evidence, assets/Wiki, graph, and one bounded Agent task.
5. Save full-page screenshots and a final artifact manifest.

