# intelifar User Guide and Module E2E Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Deliver a complete role-based Chinese user guide and executable browser evidence for every intelifar SMB product module.

**Architecture:** Add a documentation contract test, one authenticated full-module Playwright tour, and a layered Markdown handbook linked from the delivery entry points. Reuse the real gateway with isolated temporary SQLite data so the new suite never touches customer data or runtime API keys.

**Tech Stack:** Markdown, Node.js test runner, Astro, Playwright, SQLite, same-origin Node gateway.

---

### Task 1: Documentation contract

**Files:**
- Create: `site/src/scripts/user-guide-contract.test.mjs`
- Modify: `site/package.json`

**Step 1: Write the failing test**

Require `docs/INTELIFAR-USER-GUIDE.zh-CN.md` and assert all roles, internal modules, public flows, Mermaid main chain, production boundary, E2E commands and screenshot references.

**Step 2: Run test to verify it fails**

Run: `node --test src/scripts/user-guide-contract.test.mjs`
Expected: FAIL because the handbook does not exist.

**Step 3: Register the test**

Add it to the `npm test` command so omissions fail the default verification path.

### Task 2: Full-module browser tour

**Files:**
- Create: `site/e2e/module-tour.e2e.mjs`
- Modify: `site/package.json`

**Step 1: Seed isolated realistic content**

Create a temporary workspace, publication, Wiki and owner account through the same gateway/store used in production SMB mode.

**Step 2: Exercise every internal module**

Log in, traverse all nine primary navigation views, assert headings and key controls, then exercise global search, asset drawer, evidence drawer, audit filter, theme toggle, member panel and role boundary.

**Step 3: Capture guide screenshots**

Save a desktop module overview and mobile navigation image under `artifacts/user-guide-review/`.

**Step 4: Register and run**

Run: `npm run test:e2e:modules`
Expected: PASS with a module coverage summary.

### Task 3: Complete Chinese user handbook

**Files:**
- Create: `docs/INTELIFAR-USER-GUIDE.zh-CN.md`

**Step 1: Write quick start and roles**

Document login, workspace identity, owner/admin/editor/viewer/public permissions and safe credential handling.

**Step 2: Write the end-to-end flow**

Explain document intake, scanning, MinerU parsing, DeepSeek structuring, human review, publication, Wiki versioning, evidence lookup, secure share and audit.

**Step 3: Write every module with one fixed template**

Cover command center, documents, analysis, assets, Wiki, redaction/provenance, lifecycle/share, audit, system/operations, activation and public Wiki.

**Step 4: Add scenario playbooks and troubleshooting**

Include first analysis, Wiki review, member onboarding, external sharing, revocation, backup verification, failed-task retry and common error handling.

**Step 5: Add E2E and deployment boundaries**

List exact verification commands and distinguish product capability from HTTPS, AV, offsite backup, monitoring, email and provider-contract responsibilities.

### Task 4: Entry points and evidence report

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `INTELIFAR-DELIVERY.md`
- Create: `artifacts/user-guide-e2e-report.md`

Link the handbook, list module-tour evidence, record commands and results, and avoid duplicating the full manual in the delivery summary.

### Task 5: Final verification

Run:

```powershell
npm test
npm run build
npm run test:e2e
npm run test:e2e:smb
npm run test:e2e:operations
npm run test:e2e:collaboration
npm run test:e2e:modules
npm run test:e2e:real
npm audit --omit=dev
npm run security:scan
git diff --check
```

Expected: all suites PASS, 0 dependency vulnerabilities, 0 credential leak hits and no whitespace errors.
