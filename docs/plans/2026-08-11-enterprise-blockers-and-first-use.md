# Enterprise Blockers and First-use Optimization Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix the two role-based production blockers and make the existing document-IP-Wiki product understandable and truthful for first-time nontechnical enterprise users.

**Architecture:** Enforce authoritative permissions in the same-origin Node gateway, mirror those capabilities in the Astro/vanilla-JS UI, and keep state changes transactional and post-acceptance. Add deterministic UI and server validation for Wiki and Agent consistency without introducing a new workflow engine.

**Tech Stack:** Node.js, Astro, vanilla ES modules, SQLite/better-sqlite3, Node test runner, Playwright-based E2E.

---

### Task 1: Lock full-space audit to administrators

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`

**Steps:**
1. Add authenticated owner/admin and viewer fixtures asserting 200 versus 403 for `GET /api/audit`.
2. Change the audit read route to require `admin`; keep controlled event append available to viewer.
3. Hide/guard audit navigation, route, loading and CSV export for non-admin roles.
4. Run targeted server and UI contract tests.

### Task 2: Make document upload permission and state truthful

**Files:**
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`
- Modify: `site/e2e/module-tour.e2e.mjs`

**Steps:**
1. Add tests for viewer-disabled intake and Chinese 403 handling without stale task state.
2. Derive and apply the editor-or-higher analysis capability to every intake trigger.
3. Do not mutate analysis state or navigate until `/api/analysis` returns 202.
4. Clear demo category when a real file replaces the demo sample.
5. Verify editor success and viewer denial in E2E.

### Task 3: Prevent empty Wiki versions and add change preview

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**
1. Add a failing store test that an unchanged Wiki update returns `NO_CHANGES` and keeps one version.
2. Compare normalized editable fields inside the transaction before insert.
3. Add a preview panel, changed-field count and disabled save state.
4. Add input-driven preview and change-note guidance.
5. Verify V1.0 remains unchanged on empty save and a real edit creates V1.1.

### Task 4: Businessize Agent output and enforce count consistency

**Files:**
- Modify: `site/server/agent-contract.mjs`
- Modify: `site/server/agent-contract.test.mjs`
- Modify: `site/src/scripts/agent-workbench.mjs`
- Modify: `site/src/scripts/agent-workbench.test.mjs`
- Modify: `site/src/pages/index.astro`

**Steps:**
1. Add tests for a summary count that conflicts with receipt-visible asset IDs.
2. Normalize “共 N 项” to the actual visible asset count when the count is present.
3. Replace engineering labels with business labels and hide raw source IDs behind friendly buttons.
4. Rename grounded counts to “有依据条目” and separate completed from needs-review states.
5. Run Agent unit and E2E suites.

### Task 5: Align asset, risk, lifecycle, share and system wording with real behavior

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/pages/shared.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/styles/public-share.css`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`

**Steps:**
1. Convert asset import/create affordances into real document-intake actions.
2. Rename the redaction surface to risk clues and remove unsupported redacted-copy promises from real intake.
3. Split lifecycle review copy into asset and relationship counts.
4. Localize secure-share and public-page copy; clarify revocation limits and add sender-contact guidance.
5. Collapse system technical topology and present business service labels first.
6. Run contract and responsive checks.

### Task 6: Full regression and real UI evidence

**Files:**
- Create: `artifacts/enterprise-blockers-fixed-2026-08-11/report.md`
- Create: `artifacts/enterprise-blockers-fixed-2026-08-11/*.png`

**Steps:**
1. Run targeted tests after each task.
2. Run `npm test`, `npm run build`, `npm run test:e2e:modules`, `npm run test:e2e:agent`, and `npm run test:e2e:real-ui`.
3. Start/refresh the real UI on port 4388.
4. Verify owner/editor/viewer/external flows in the real browser and capture screenshots.
5. Document outcomes, remaining production responsibilities and any non-blocking residuals.

