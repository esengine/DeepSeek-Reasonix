# Enterprise Action Center, Search and Wiki Approval Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a role-aware action center, cross-Wiki global search, persistent Wiki publication review, and business-first operation ledger.

**Architecture:** Derive ordinary action items from existing authoritative workspace data and persist only Wiki review requests. Reuse `/api/search` for permission-filtered cross-Wiki lookup and the existing append-only audit ledger for workflow evidence.

**Tech Stack:** Astro, browser-native JavaScript modules, Node.js test runner, better-sqlite3, loopback HTTP server, in-app browser E2E.

---

### Task 1: Test and implement workspace presentation helpers

**Files:**
- Create: `site/src/scripts/workspace-experience.mjs`
- Create: `site/src/scripts/workspace-experience.test.mjs`
- Modify: `site/package.json`

**Steps:**
1. Write failing tests for role-aware action derivation, duplicate-free search presentation, and audit category mapping.
2. Run `node --test src/scripts/workspace-experience.test.mjs` and confirm failure.
3. Implement minimal pure helpers.
4. Run the focused tests and confirm pass.

### Task 2: Persist Wiki publication reviews

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/server/publication-registry.mjs`
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`

**Steps:**
1. Add failing store tests for submit, list, approve, reject and version conflict behavior.
2. Add `wiki_review_requests`, row mapping, prepared statements and transactional methods.
3. Add failing HTTP tests for editor submission, viewer denial and administrator decision.
4. Add role-protected review endpoints and append audit events.
5. Run focused store/server tests.

### Task 3: Build action center and global search UI

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**
1. Add semantic action-center markup, filters, badges and empty/loading states.
2. Render derived actions after authoritative data loads and wire destination buttons.
3. Replace Enter-only asset search with an accessible global search result panel backed by `/api/search`.
4. Cover keyboard navigation, loading, empty, error and mobile states.

### Task 4: Connect Wiki approval and business operation records

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`

**Steps:**
1. Make the Wiki edit submit label and endpoint role-aware.
2. Render pending Wiki reviews in the action center and wire approve/reject actions.
3. Rename the audit surface to operation records and map events into four business categories.
4. Keep event IDs and integrity state while removing technical implementation names from the default view.

### Task 5: Automated and real-UI verification

**Files:**
- Modify: `site/e2e/module-tour.e2e.mjs`
- Update: `artifacts/user-guide-review/*.png`

**Steps:**
1. Add E2E assertions for action filters, global search result opening, Wiki approval affordance and operation-category filtering.
2. Run `npm test`, `npm run build`, `npm run test:e2e:modules`, and `npm run security:scan`.
3. Restart the loopback server on port 4388 if required.
4. Use the real UI to verify desktop/mobile states, screenshots and browser console output.
