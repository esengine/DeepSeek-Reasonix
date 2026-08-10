# Windows Native Full CI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run uncached Root, SDK, and Desktop Go tests on an isolated native Windows GitHub runner without WSL or host antivirus exceptions.

**Architecture:** A dedicated pull-request/manual GitHub Actions workflow prepares the pinned Go and frontend toolchains, validates Git for Windows Bash, runs every module while preserving later-module execution after failures, uploads logs unconditionally, and applies one final aggregate gate.

**Tech Stack:** GitHub Actions, `windows-latest`, Go 1.26.5, Git for Windows Bash, Node.js 24, pnpm 10, Wails 2.12.0.

---

### Task 1: Add the isolated workflow

**Files:**
- Create: `.github/workflows/windows-native-full.yml`

**Step 1: Define triggers and least privilege**

Add `workflow_dispatch`, pushes to fork feature branches (`feature/**`), and pull requests targeting `main-v2`; set `permissions: contents: read`, and use a concurrency group that cancels superseded runs. The push trigger supplies first-run evidence while the new workflow is not yet present on the upstream default branch.

**Step 2: Prepare pinned toolchains**

Check out source, use root `go.mod` with `actions/setup-go`, install pnpm and Node, and explicitly set `GOTOOLCHAIN=local`.

**Step 3: Validate Git Bash**

Resolve `bash` with `cygpath -w`, require a Git installation path, and reject `Windows\\System32\\bash.exe`.

### Task 2: Run all modules and retain evidence

**Files:**
- Modify: `.github/workflows/windows-native-full.yml`

**Step 1: Run Root and SDK**

Use `-count=1`, bounded per-package timeouts, `tee`, and `continue-on-error` for each independent module.

**Step 2: Build Desktop prerequisites**

Install Wails 2.12.0, generate Wails bindings, install the frozen frontend lockfile, and build the frontend while logging the full step.

**Step 3: Run Desktop**

Run the complete Desktop module serially at package level with `-p 1 -count=1 -timeout=12m`.

**Step 4: Upload and aggregate**

Upload all logs under `if: always()` and fail a final PowerShell gate unless every required outcome is `success`.

### Task 3: Validate without executing Go on the host

**Files:**
- Test: `.github/workflows/windows-native-full.yml`

**Step 1: Parse YAML**

Use the bundled workspace Python/YAML runtime to parse the workflow and assert its top-level structure, permissions, runner, module commands, uncached flags, and unconditional log upload.

**Step 2: Inspect repository state**

Run `git diff --check` and review the workflow diff. Do not run Go build/test on the host.

### Task 4: Publish only with an authorized remote

**Files:**
- No source changes expected.

**Step 1: Inspect remote capability**

Use a non-mutating remote query or dry-run push. Do not expose credentials and do not push to upstream without write authorization.

**Step 2: Trigger CI**

Push the branch to a confirmed writable remote and open a PR to `main-v2`, or dispatch the workflow after it exists on the repository default branch.

**Step 3: Retain final evidence**

Download/record the GitHub Actions run URL, per-module outcomes, and artifact structure in `artifacts/windows-native-go-tests/summary.md`.
