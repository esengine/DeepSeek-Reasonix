# Windows Native Full Go Tests Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make all Go tests repeatably executable on native Windows without WSL, Docker, skipped cases, or persistent firewall changes.

**Architecture:** A non-admin PowerShell orchestrator resolves the pinned Go toolchain and Git for Windows Bash, starts a short-lived elevated firewall lease broker for two loopback-only rules, runs all three Go modules itself with uncached tests, records per-module logs and a Markdown summary, and signals the broker to remove the rules in a `finally` block.

**Tech Stack:** PowerShell 5.1+, Go 1.26.5, Git for Windows Bash, Windows Defender Firewall.

---

### Task 1: Add the native Windows runner contract

**Files:**
- Create: `scripts/test-go-windows-native.ps1`
- Modify: `README.zh-CN.md`

**Step 1: Implement preflight and strict tool resolution**

Resolve the repository root, portable or system Go, and Git Bash. Reject `System32\bash.exe` and expose a `-PreflightOnly` mode that performs no system mutation.

**Step 2: Verify preflight**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-go-windows-native.ps1 -PreflightOnly`

Expected: PASS with Go 1.26.5 and a Git for Windows Bash path; no UAC and no firewall rule.

### Task 2: Add scoped elevation and guaranteed cleanup

**Files:**
- Modify: `scripts/test-go-windows-native.ps1`

**Step 1: Implement UAC broker launch**

Launch the same script with an internal broker flag. The elevated process only owns firewall rules; tests remain in the original non-admin process.

**Step 2: Implement temporary loopback rules**

Create unique IPv4 and IPv6 inbound TCP allow rules restricted on both ends to loopback. Record exact names and delete them in `finally`, including after failed tests, parent-process exit, or lease timeout.

**Step 3: Add cleanup verification**

Query the exact names after removal. Cleanup failure must make the runner fail.

### Task 3: Execute every Go module and retain evidence

**Files:**
- Modify: `scripts/test-go-windows-native.ps1`

**Step 1: Normalize the child environment**

Prepend Git Bash and Go to PATH, set local toolchain/module proxy/build flags, increase only the Windows sandbox readiness allowance, and unset the optional live provider cache guard in the child process.

**Step 2: Run all three modules**

Run root, `sdk/go`, and `desktop` with `-count=1`. Continue after an individual failure and retain each complete log.

**Step 3: Generate the summary**

Write module status, command, duration, and log path to `artifacts/windows-native-go-tests/summary.md`; exit nonzero if any module or cleanup fails.

### Task 4: Verify the former failure classes

**Files:**
- No source changes expected.

**Step 1: Run the desktop focused regression tests under the native runner environment**

Expected: all three quick-click workspace reconciliation tests and the Windows packager Bash test pass.

**Step 2: Run the complete native suite**

Run: `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/test-go-windows-native.ps1`

Expected: root, SDK, and desktop all PASS; no WSL process is started; no temporary firewall rule remains.

### Task 5: Inspect and commit

**Files:**
- Modify: `artifacts/windows-native-go-tests/summary.md` only as generated verification evidence.

**Step 1: Inspect changes**

Run: `git diff --check` and `git status --short`.

**Step 2: Commit**

Commit the design, plan, runner, documentation, and final verification evidence with an intentional Windows-test message.
