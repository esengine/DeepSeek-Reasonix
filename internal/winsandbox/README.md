# internal/winsandbox

Reasonix's bundled native Windows process sandbox helpers.

This package provides a small Windows-only sandbox runner built from platform
primitives:

- AppContainer for read-only launches.
- Low-integrity primary tokens for writable launches.
- Temporary ACL grants for writable roots, command temp roots, and executable
  roots.
- Temporary deny ACEs for forbid-read roots (files and directories).
- Per-command temp directory redirection.
- Kill-on-close Job Objects for process-tree cleanup.
- Per-root serialization so concurrent runs against a shared workspace cannot
  corrupt each other's ACL/label boundary.
- Best-effort cleanup of ACL/integrity-label residue left by a crashed run.

The package intentionally exposes a narrow API. It does not implement product
policy, prompting, or shell parsing; callers pass an already-resolved argv and a
small filesystem/network policy.

## Usage

```go
result, err := winsandbox.Run(winsandbox.Spec{
    WritableRoots:   []string{workspace},
    ForbidReadRoots: []string{secretDir},
    Network:         true,
    Writable:        true,
    TempPrefix:      "myapp-sandbox-",
}, []string{"powershell", "-NoProfile", "-NonInteractive", "-Command", script}, winsandbox.RunOptions{
    Stdin:  os.Stdin,
    Stdout: os.Stdout,
    Stderr: os.Stderr,
})
```

## Concurrency And Crash Safety

The sandbox enforces its boundary by temporarily mutating a path's DACL and
integrity label and restoring them afterward, so two runs touching the same root
must not overlap. Each run takes a per-root named mutex (session-local
namespace) for its whole lifetime; runs on disjoint roots proceed in parallel,
runs on a shared root serialize. A crashed holder never deadlocks the next run
because the OS marks its mutex abandoned. The holder records its PID and a
command preview next to each lock, so a queued run's notice and timeout error
name what is blocking it. The queue wait defaults to 1 minute (an interactive
command should fail fast with the holder named, not hang); a caller whose run
nobody is blocked on — a background job — passes a longer `Spec.LockWait`
budget.

A writable run stamps a Low integrity label across its writable subtree and adds
a deny ACE (including the current user's SID) for each forbid-read root. Both are
undone on normal cleanup. If a run is force-killed before cleanup, the next run
sweeps the residue: the writable subtree is recursively reset to Medium
integrity, and lingering sandbox deny ACEs recorded by a now-dead process are
removed, so a crash cannot permanently lower a workspace's integrity or lock the
user out of a forbid-read path such as `~/.ssh`.

## Environment Overrides

| Variable | Effect |
| --- | --- |
| `WINDOWS_SANDBOX_WAIT_MS` | Max wall-clock a sandboxed child may run before it is killed. |
| `WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS` | `icacls` timeout. Recursive (`/T`) operations default to a much larger ceiling than flat ones because they walk the whole subtree; this overrides both. |
| `WINDOWS_SANDBOX_LOCK_MS` | Max wall-clock to wait for a busy per-root lock before failing with a clear error instead of hanging. Overrides both defaults (1 minute interactive, 10 minutes for background bash jobs); stop the command named in the error before raising this. |

## Network Semantics

Read-only launches use AppContainer. When `Network` is false, network
capabilities are omitted.

Writable launches use a low-integrity token so normal developer workspaces can
be written without requiring an elevated helper. Low-integrity tokens do not
provide reliable per-process network blocking without elevated firewall or WFP
setup, so writable launches with `Network: false` fail closed.

## Platform Support

`Available` and `Run` return unavailable on non-Windows hosts. The module still
builds on non-Windows platforms so callers can depend on it unconditionally.

## Verification Matrix

The Windows CI tests exercise both sandbox launch modes:

- writable low-integrity commands can write inside configured roots and command
  temp, but not outside configured roots;
- read-only AppContainer commands can read allowed roots but cannot write them;
- `ForbidReadRoots` are denied in both writable and read-only launches;
- `Network: false` AppContainer launches cannot connect to a loopback listener;
- stdin, stdout, stderr, environment overrides, working directory, paths with
  spaces, and child exit codes are preserved;
- kill-on-close Job Objects clean up child process trees;
- temporary ACL grants/denies are removed after the command exits, leaving no
  Low integrity label or sandbox deny ACE behind;
- concurrent writable commands against a shared non-empty workspace all succeed
  and leave no residue (serialization regression guard).
