# intelifar SMB P0-D Collaboration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver self-service member onboarding and genuinely revocable, auditable redacted-Wiki sharing so the single-instance SMB MVP exceeds 98/100.

**Architecture:** Extend the existing SQLite store with invitation and secure-share records, expose tightly authorized same-origin APIs plus two narrow public capability endpoints, and replace lifecycle demo state with real server data. Keep all secrets one-time-only and store only hashes.

**Tech Stack:** Node.js 22, Astro 7, better-sqlite3 12.10.0, Node test runner, Playwright.

---

### Task 1: Member and invitation persistence

**Files:**
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/server/auth-service.mjs`
- Modify: `site/server/auth-service.test.mjs`

**Steps:**
1. Write failing tests for workspace-scoped member listing, invite uniqueness, one-time consumption, owner protection, role updates and session revocation.
2. Add invitation tables, parameterized statements and transactional store operations.
3. Add auth-service invite creation/acceptance validation using high-entropy token hashes and scrypt passwords.
4. Run focused tests and inspect database rollback behavior.

### Task 2: Member administration APIs

**Files:**
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/smb-platform-server.test.mjs`

**Steps:**
1. Add public invitation inspection/acceptance before the authenticated `/api/*` gate.
2. Add owner/admin member and invitation list/create/update endpoints.
3. Enforce no invited owner, no owner mutation, no self-disable, strict role and email validation.
4. Add generic public errors, rate limits and hash-chain audit events.

### Task 3: Secure share persistence and public access

**Files:**
- Create: `site/server/share-service.mjs`
- Create: `site/server/share-service.test.mjs`
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/smb-platform-server.test.mjs`

**Steps:**
1. Write failing tests for double-secret storage, workspace isolation, expiry, revocation, bad-code handling and redacted output allowlist.
2. Persist only SHA-256 token/code hashes and workspace-scoped share metadata.
3. Add authenticated create/list/revoke APIs and rate-limited public inspect/access APIs.
4. Append successful accesses to the workspace audit chain without storing secrets.

### Task 4: Internal member and share UI

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`

**Steps:**
1. Add a Team Access ledger and invitation dialog to the System view.
2. Replace local-only share creation/listing with same-origin server APIs and one-time credential display.
3. Add revoke, role/state, access count, expiry and copy actions using text-only DOM rendering.
4. Keep static demo behavior explicitly labeled and preserve mobile accessibility.

### Task 5: Public redacted Wiki viewer

**Files:**
- Create: `site/src/pages/shared.astro`
- Create: `site/src/scripts/public-share.mjs`
- Create: `site/src/styles/public-share.css`
- Create: `site/src/pages/activate.astro`
- Create: `site/src/scripts/invite-activation.mjs`

**Steps:**
1. Build a branded locked-viewer page that reads the token from the URL fragment, keeping it out of server logs and referrers.
2. Submit the access code to the public API and render only the redacted allowlist.
3. Build an invitation activation page with strong-password validation and generic failure states.
4. Verify keyboard, mobile and no-internal-data contracts.

### Task 6: E2E, score and delivery evidence

**Files:**
- Create: `site/e2e/smb-collaboration.e2e.mjs`
- Create: `artifacts/smb-p0d-report.md`
- Modify: `site/package.json`
- Modify: `INTELIFAR-DELIVERY.md`
- Modify: `artifacts/delivery-tree.txt`

**Steps:**
1. Browser-test owner login, invite creation, invite activation, member login, share creation, public unlock, access audit and revoke denial.
2. Assert editor/viewer/admin authorization boundaries and that API/database payloads never contain raw tokens or access codes.
3. Capture internal desktop, public viewer and mobile screenshots plus refreshed structure evidence.
4. Run all unit/API/E2E/build/security/audit/live-provider checks.
5. Re-score with evidence; mark 98+ only if all required flows pass.
