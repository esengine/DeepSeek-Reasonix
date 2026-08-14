# intelifar SMB P0-C Security Operations Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a truthful, tested security-operations loop for document quarantine, verified SQLite backups, and administrator job recovery.

**Architecture:** Add a layered file-security adapter ahead of MinerU, a backup service around the existing SQLite store, and owner/admin operations APIs consumed by the existing System view. Keep restore offline and destructive by design.

**Tech Stack:** Node.js 22, Astro 7, better-sqlite3 12.10.0, Node test runner, Playwright.

---

### Task 1: File security preflight and scanner adapter

**Files:**
- Create: `site/server/file-security-service.mjs`
- Create: `site/server/file-security-service.test.mjs`
- Modify: `site/server/zip-reader.mjs`
- Modify: `site/server/zip-reader.test.mjs`

**Steps:**
1. Write failing tests for EICAR, disguised PE, Office macro, dangerous ZIP entry, compression-ratio limit, clean input, optional external scanner, and fail-closed mode.
2. Add bounded ZIP central-directory inspection without extracting attacker-controlled content.
3. Implement deterministic preflight and an injectable/ClamAV-compatible external scanner adapter.
4. Run the focused tests and inspect failures before continuing.

### Task 2: Quarantine the analysis pipeline

**Files:**
- Modify: `site/server/analysis-service.mjs`
- Modify: `site/server/analysis-service.test.mjs`
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`

**Steps:**
1. Add security metadata to the public job model and make `blocked` terminal.
2. Run the scanner before MinerU and persist `security-scan`, `blocked`, or retryable scanner-unavailable outcomes.
3. Delete malicious quarantined files; retain transient-failure uploads for retry.
4. Add truthful scanner posture to `/api/health` and preserve provider isolation tests.

### Task 3: Verified SQLite backups and admin APIs

**Files:**
- Create: `site/server/backup-service.mjs`
- Create: `site/server/backup-service.test.mjs`
- Modify: `site/server/platform-store.mjs`
- Modify: `site/server/platform-store.test.mjs`
- Modify: `site/server/real-analysis-server.mjs`
- Modify: `site/server/real-analysis-server.test.mjs`

**Steps:**
1. Expose a narrow online-backup and integrity-check interface from the store.
2. Generate a temporary backup, validate it read-only, hash it, write a manifest, and atomically publish it.
3. Enforce retention and strict server-generated backup identifiers.
4. Add owner/admin-only operations, create-backup, and verify-backup APIs with audit events.

### Task 4: Administrator operations UI

**Files:**
- Modify: `site/src/pages/index.astro`
- Modify: `site/src/scripts/ip-platform.mjs`
- Modify: `site/src/styles/ip-platform.css`
- Modify: `site/src/scripts/ip-platform-contract.test.mjs`

**Steps:**
1. Add an operations ledger to the System view using existing intelifar visual tokens.
2. Load real scanner, backup, audit, and job data for owner/admin sessions.
3. Implement backup creation, backup verification, and failed/interrupted job retry with accessible status feedback.
4. Keep unsupported roles and offline demo behavior explicit.

### Task 5: Browser E2E, screenshots, and delivery evidence

**Files:**
- Create: `site/e2e/smb-operations.e2e.mjs`
- Create: `artifacts/smb-p0c-report.md`
- Modify: `site/package.json`
- Modify: `INTELIFAR-DELIVERY.md`

**Steps:**
1. Test malicious upload isolation through the real HTTP API and assert providers were never invoked.
2. Test owner login, operations status, backup creation/verification, and job retry in a real browser.
3. Capture desktop and mobile System-view screenshots.
4. Run unit/API tests, offline E2E, SMB E2E, the new operations E2E, build, credential scan, npm audit, and real MinerU + DeepSeek E2E.
5. Record exact evidence and remaining deployment responsibilities without claiming external AV coverage when unavailable.
