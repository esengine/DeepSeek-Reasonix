# Windows sandbox architecture

Reasonix uses the same restricted-token pattern as DeepSeek Harness commit
`c291e7961a` for shell and writer processes on Windows. The implementation is a
native Go port; it does not load Harness code at runtime.

## Permission mapping

| Permission preset | Windows execution |
| --- | --- |
| Read only | A `WRITE_RESTRICTED` primary token with no directory capability SID. Direct read-only tools may use AppContainer. |
| Workspace write | A `WRITE_RESTRICTED` token containing only the capability SIDs for the canonical workspace, approved extra directories, and the session-private temp directory. |
| Full access | The normal host execution path. Explicit deny rules and protected application state still apply above the process sandbox. |

The backend is reported as `windows-write-restricted+appcontainer` with
`partial` enforcement. Capability snapshots also report the write, read, and
network isolation mechanisms independently so a desktop or remote client does
not mistake the Windows boundary for Seatbelt or bubblewrap parity.

## Write-restricted shell lane

For each canonical writable directory, Reasonix derives a deterministic
capability SID from a Reasonix-specific domain, the capability purpose, and the
case-folded path. A separate purpose derives the session-temp SID. The helper
adds one object-and-container-inheritable Modify-class ACE to the exact
directory. The mask excludes `WRITE_DAC` and `WRITE_OWNER`.

The ACE is standing state, while authority is per process. A subsequent token
can use an ACE only when the host includes that exact SID in its restricting
SID set. Downgrading to read only, revoking an extra directory, switching a
session, or rotating the session temp omits the old SID and makes its ACE inert.
The helper revalidates the directory's Windows file identity after changing its
DACL and fails closed if the path was replaced.

Before changing a workspace DACL, the helper atomically writes a `preparing`
record below the host-controlled Reasonix state root. After the exact ACE and
file identity are verified, the same record becomes `active`. It contains the
record version, canonical path, Windows object identity, SID, exact mask,
inheritance, purpose, and owner process. Session-temp records are deliberately
not persisted because the random generation directory and its ACE are deleted
together.

The restricted token is created with `DISABLE_MAX_PRIVILEGE`, `LUA_TOKEN`, and
`WRITE_RESTRICTED`. Its restricting set includes the logon SID and Everyone so
PowerShell, DLL, CNG, pipe, and desktop initialization can succeed, plus the
authorized capability SIDs in workspace-write mode. The default DACL uses the
session-temp capability where available. The child starts suspended, is
assigned to a kill-on-close Job Object with UI restrictions, and is then
resumed. Closing or timing out the run terminates the process tree.

`CREATE_NO_WINDOW` is intentionally omitted for this lane because restricted
PowerShell and Node initialization rely on normal console inheritance. The
Reasonix helper process remains hidden by the desktop process launcher.

## Direct-tool and protected-read lane

The existing AppContainer path remains for direct read-only tools and is the
only Windows lane that can remove network capabilities. Existing
`forbid_read` roots remain temporary deny ACEs because `WRITE_RESTRICTED` does
not isolate reads. Those mutations are snapshotted, crash-marked, restored, and
serialized for their mutation lifetime.

Standing capability ACE creation uses short named-mutex critical sections.
Parent and child paths share a lock domain, preventing concurrent DACL updates
from losing an ACE. Ordinary commands sharing a workspace run concurrently
after the exact ACE exists.

Protected Reasonix state cannot be inside a granted workspace, and arbitrary
state descendants cannot become writable roots. The one explicit exception is
the desktop's direct `<state>/global-workspace`: its ACE is placed on that exact
descendant and does not grant its parent. Session temp must be disjoint from
both workspace and protected roots.

## Migration from the earlier Windows backend

The previous shell path used a low-integrity token and recursively changed
workspace integrity labels. The new shell path does not relabel the workspace
and does not restore large descriptor trees after each command. Existing
AppContainer compatibility code may still use its historical ACL and integrity
label handling for direct tools.

Old low-integrity labels are not required by the new shell lane. Capability
ACEs are harmless without a token carrying their derived SID and may remain on
a directory across upgrades. Removing a workspace or session-temp directory
removes the corresponding ACE with it.

The helper payload is versioned and rejects missing versions, future versions,
unknown fields, and trailing data. A missing native API, path-identity mismatch,
protected-root conflict, or unsupported network request returns a structured
sandbox failure; Reasonix does not silently rerun the command without a sandbox.

## Known Windows boundaries

- Restricted tokens isolate write-class file access. They do not isolate
  ordinary reads, network, registry, named objects, or process visibility.
- Everyone remains in the restricting set for Windows runtime compatibility.
  A location that explicitly grants ambient writes to Everyone may remain
  writable.
- NTFS hard links and filesystem objects with unusual ACL inheritance require
  native regression coverage. Reasonix revalidates directory identity but does
  not claim a general hard-link boundary.
- AppContainer can disable network for direct tools. Shell and writer launches
  with `Network: false` fail closed because the restricted-token lane cannot
  enforce that boundary.
- Windows therefore remains `partial`, including when every native API is
  available.

## Validation

Run the native suite and the 100-launch cold/warm benchmark on each supported
Windows architecture:

```powershell
./scripts/verify-windows-sandbox.ps1 -OutputDirectory .codex-build/windows-sandbox-native
```

CI includes `reasonix/internal/winsandbox` in the Windows smoke group. Non-Windows
hosts also cross-compile `internal/winsandbox` and `internal/sandbox` for
Windows amd64 and arm64. Cross-compilation validates bindings and build tags;
it is not a substitute for the native ACL, token, Job Object, PowerShell, and
Electron checks described in the evidence README.

The ported design is derived from DeepSeek Harness' MIT-licensed
`packages/sandbox/sandbox-windows-acl`; attribution is recorded in
`internal/winsandbox/NOTICE.md`.
