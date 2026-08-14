# Real UI Port E2E Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a stable browser E2E contract for the real UI default port 4388 and execute all upstream Go modules with the pinned toolchain.

**Architecture:** A standalone Node E2E owns the real gateway child process, uses placeholder provider credentials, probes health, and drives installed Chromium through the three critical routes. Go is supplied as a verified portable toolchain outside the repository so test execution does not require an administrator or alter the machine-wide PATH.

**Tech Stack:** Node.js test runner scripts, Playwright with installed Chrome, Astro production build, Go 1.26.5.

---

### Task 1: Add the failing default-port E2E contract

**Files:**
- Create: `site/e2e/real-ui-default-port.e2e.mjs`

**Step 1: Write the E2E**

Spawn the CLI entry with `PORT` removed, wait for the exact 4388 startup URL, assert `/api/health`, and use Chromium to assert `#assets` and `#agent` views.

**Step 2: Run it against the old default**

Run: `node site/e2e/real-ui-default-port.e2e.mjs`

Expected before the implementation change: FAIL because the process listens on 4322. Expected in the current worktree: PASS because the port implementation has already been changed to 4388.

### Task 2: Make the E2E part of normal verification

**Files:**
- Modify: `site/package.json`
- Modify: `INTELIFAR-DELIVERY.md`

**Step 1: Add the script**

Add `test:e2e:real-ui` and invoke it from both `verify` and `verify:real` after the production build.

**Step 2: Run the focused test**

Run: `npm run test:e2e:real-ui`

Expected: PASS with health, home, panorama, and Agent assertions.

### Task 3: Verify the complete JavaScript delivery gate

**Files:**
- No source changes expected.

**Step 1: Run verification**

Run: `npm run verify`

Expected: unit/contract tests, production build, default-port E2E, main browser E2E, and bounded Agent E2E all pass.

### Task 4: Provision and verify the pinned Go toolchain

**Files:**
- No repository files; install under the user-local toolchain cache.

**Step 1: Download and verify**

Download `https://go.dev/dl/go1.26.5.windows-amd64.zip` and require SHA-256 `97e6b2a833b6d89f9ff17d25419ac0a7e3b482a044e9ab18cdef834bd834fd38` before extraction.

**Step 2: Run all upstream modules**

Run from the repository root: `go test ./...`

Run from `sdk/go`: `go test ./...`

Run from `desktop`: `go test ./...`

Expected: all packages pass; any platform-specific failure is reported with its exact package and test name.

### Task 5: Record and deliver results

**Files:**
- Modify: `INTELIFAR-DELIVERY.md` only if the final commands differ from the documented contract.

**Step 1: Inspect changes**

Run: `git diff --check` and `git status --short`.

Expected: no whitespace errors and only intentional files changed.

**Step 2: Commit**

Commit the port implementation, E2E, package scripts, design, plan, and documentation with an intentional test-focused message after all gates complete.
