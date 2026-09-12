# internal/winsandbox

Reasonix's bundled native Windows process sandbox helpers.

The package combines two Windows isolation lanes:

- AppContainer for direct read-only tools, including optional network removal.
- `WRITE_RESTRICTED` primary tokens for shells and writer processes.
- Deterministic, per-directory capability SIDs for workspace writes.
- A distinct capability SID for the session-private temporary directory.
- Temporary deny ACEs for `ForbidReadRoots`.
- Kill-on-close Job Objects and UI restrictions for process-tree cleanup.
- Strict, versioned helper payloads and fail-closed native API handling.

The package implements enforcement only. Product permission presets, approval
prompts, and grant policy live above this layer.

## Write capability model

For a workspace-write shell, Reasonix derives a capability SID from each
canonical writable root and adds a `Modify`-class inheritable ACE for that SID.
The ACE intentionally excludes `WRITE_DAC` and `WRITE_OWNER`. It remains on the
directory, so later commands hit an exact-ACE fast path instead of recursively
rewriting a large workspace on every launch.

Each restricted token contains the logon SID, Everyone, and only the directory
capabilities authorized for that call. Windows performs the ordinary access
check and the restricting-SID check; a write succeeds only when both pass.
Read-only shells carry no directory capability, so standing workspace ACEs are
inert after a mode downgrade.

The session temp directory uses a different SID namespace. Two sessions can
share workspace authority without receiving access to one another's private
temporary files. Deleting the session temp tree removes that temporary grant.

`ProtectedWriteRoots` are checked before any grant is materialized. A writable
root that contains protected state, an arbitrary protected-state descendant,
or an overlapping session temp is rejected. The direct
`<state>/global-workspace` child is the sole product-layout exception; its ACE
is applied only to that exact directory.

Before a workspace ACE is applied, a versioned `preparing` record is written
under the protected state root. It is atomically replaced with `active` only
after the exact ACE and Windows file identity have been revalidated.

```go
result, err := winsandbox.Run(winsandbox.Spec{
    WritableRoots:       []string{workspace},
    ProtectedWriteRoots: []string{reasonixState},
    ForbidReadRoots:     []string{secretDir},
    TempDir:             sessionTemp,
    Network:             true,
    Writable:            true,
}, argv, winsandbox.RunOptions{
    Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
})
```

Set `ReadOnly: true` for a shell that may read with the caller's normal access
but must not receive workspace or temp write capabilities. Set `Writable:
false` for the direct AppContainer lane.

## Concurrency and crash safety

Standing capability ACEs are materialized under short, per-root named mutexes.
Commands sharing a workspace can then run concurrently without grant/restore
races. `ForbidReadRoots` still use temporary current-user deny ACEs, so those
paths remain locked for the full command. A crash marker lets the next run
remove a deny left by a dead helper.

The legacy AppContainer compatibility path retains its existing ACL residue
sweeper for direct tools. The `WRITE_RESTRICTED` lane never lowers the process
integrity level and never recursively relabels the workspace Low/Medium.

## Network and read boundaries

AppContainer omits network capabilities when `Network` is false.
`WRITE_RESTRICTED` controls write-class access only; it does not isolate reads,
network, registry access, or process visibility. A restricted-token launch with
`Network: false` therefore fails closed. Reasonix reports Windows enforcement as
`partial` and retains `ForbidReadRoots` for selected sensitive paths.

Everyone remains in the restricting set because Windows shell, DLL, CNG, pipe,
and desktop initialization depends on it. A location that grants ambient write
access directly to Everyone can remain writable; this is a documented Windows
boundary of the approach.

## Environment overrides

| Variable | Effect |
| --- | --- |
| `WINDOWS_SANDBOX_WAIT_MS` | Maximum runtime before the Job Object is terminated. |
| `WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS` | Timeout for legacy AppContainer ACL cleanup operations. |
| `WINDOWS_SANDBOX_LOCK_MS` | Maximum wait for a capability or temporary-deny root lock. |

## Verification

Windows-native tests cover workspace and private-temp writes, outside and
sibling-temp denial, read-only capability downgrades, protected-root overlap,
forbid-read cleanup, AppContainer network denial, stdio/environment/cwd, timeouts,
and Job Object process-tree termination. Cross-compilation on non-Windows hosts
verifies all Win32 bindings and helper wiring.
