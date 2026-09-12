# Windows sandbox validation evidence

This directory tracks the validation contract for the WriteRestricted Windows
sandbox port. Checked-in logs must identify the exact commit, Windows release,
architecture, Go version, and command. Do not label cross-compiled binaries as
native test results.

## Required native matrix

| Environment | Required checks | Current checked-in evidence |
| --- | --- | --- |
| Windows 11 amd64 | winsandbox/sandbox tests, 100 cold and warm launches, actual Electron shell flow | Not run in this macOS worktree |
| Windows 11 arm64 | winsandbox/sandbox tests, 100 cold and warm launches, actual Electron shell flow | Not run in this macOS worktree |
| Windows 10 or supported Server amd64 | winsandbox/sandbox tests, PowerShell and background process-tree termination | Not run in this macOS worktree |

Run `scripts/verify-windows-sandbox.ps1` on each machine and attach the emitted
metadata, test, and benchmark logs here or to the release artifact. The native
tests cover workspace/outside writes, session-temp separation, read-only mode,
standing-ACE authorization, protected roots, forbid-read cleanup, AppContainer
network denial, path replacement checks, concurrent commands after short ACL
setup, PowerShell 5/7 and available Node/Python inline runtimes,
stdio/environment/cwd, timeout, and Job Object process-tree cleanup.

The current non-Windows validation consists of Go unit tests for portable
projection/protocol logic plus Windows amd64 and arm64 cross-compilation. It
does not exercise ACL evaluation or Windows process creation.
