# Session ownership, rewind, and worktree fallback

How Reasonix decides who may write a session, how conflicts are saved, and
how rewind and workspace isolation interact.

## Session writers

One session file has one cross-process writer at a time. The ticket and holder
metadata share one session lease file (`.lease.lock`). Production controllers
bind a generation-bound `SessionWriter`; a rebind invalidates every older
generation. Legacy `.lease.json` metadata remains read-only compatible.

Intentional path changes (`new`, `clear`, `fork`, `branch`, and `switch`) use a
prepare-before-publish handoff: the frontend acquires the target lease and binds
the unpublished Session before the controller swaps paths. Failure leaves the
source intact for non-destructive transitions; `clear` never publishes an
unleased replacement.

Every save also takes the bounded `.jsonl.lock` compatibility flock. The
session lease decides who may own a live transcript; the save lock keeps that
owner serialized with supported older binaries and one-shot recovery/import
writers that do not yet participate in the lease protocol.

The event log (`.events.jsonl`) is the source of truth. Writer-bound saves CAS
against the log tail (size + index revision/digest) and a paired in-memory
transcript view. `.jsonl` remains a compatibility projection.

## Conflicts

1. Event-log tail still matches this writer → normal save (no-op / append / replace).
2. Disk already covers the local prefix → adopt disk, no branch.
3. True divergence, replaced log, or deleted original → one stable recovery
   file keyed by root branch ID + the live Session's first writer generation.
   Lease rebinds keep that lane; later conflicts update the same path. There is
   no recovery-on-recovery chain.

## Rewind

- **Code**: restore file before-images. Already-restored files (current ==
  before) are skipped. External changes refuse overwrite.
- **Conversation**: fork a new session. The parent transcript is never
  truncated.
- **Both**: fork first, then restore files. A file conflict keeps the new
  branch and reports `partial=true`.

New checkpoints write `turns/<turn>/meta.json` plus raw `files/NNNN.before`
payloads (schema v3). The newest 100 turn directories are retained by default;
new checkpoint payloads are not duplicated into blobs. v1/v2 `turn-N.json`
files and their legacy blobs remain readable.

The v2 compatibility marker is also the v3 turn's liveness record. A previous
binary that truncates `turn-N.json` therefore tombstones the matching v3
directory; upgrading again cannot resurrect the removed future turns.

Structured writers (`write_file`, `edit_file`, `multi_edit`, notebook edit)
re-check existence, SHA-256, and mode before publish. A mismatch returns
`ErrFileChanged`.

## Worktree fallback

Forking from a message offers two workspace policies. **Conversation only
(shared)** keeps the source workspace, including its current uncommitted files.
**Isolated worktree** creates a durable `reasonix/delivery-*` branch from the
repository's committed `HEAD`, opens the fork as a registered project, and
keeps the source checkout unchanged. Because Git worktrees do not copy local
changes, Reasonix requires a clean source checkout for this combined fork. A
dirty checkout is refused with guidance to commit/stash or use the shared fork.

If the folder is not a Git project or worktree prerequisites are unavailable,
Reasonix creates the conversation fork in the shared workspace and reports the
fallback. If conversation creation or tab attachment fails after a worktree was
created, automatic cleanup removes it only while its branch, `HEAD`, and status
still match the untouched creation result. Any detected change preserves the
worktree for recovery. A successfully attached worktree remains registered
across tab close/restart and is never deleted automatically; remove its Git
worktree and branch explicitly when finished.

Delivery worktrees stay optional. Non-isolated directories use the workspace
lease (`filelock`). Path-bound writes take shared ancestor compatibility locks,
shared hierarchy stripes through the concrete path, and an exclusive file
stripe for the duration of that tool. Whole-workspace writers take their exact
root and hierarchy stripe exclusively. Parent workspaces and directly opened
nested repositories therefore intersect, while two sessions can still write
different files (including in the same repo) at once. `bash`/MCP mutations take
the whole-workspace locks only for that command. Any tool call does the same
when a configured tool hook may write undeclared paths. File and hierarchy
identities map into bounded stripe sets; collisions may serialize unrelated
work but cannot weaken protection. Read-only bash does not take a write lease.
On macOS, folded domains coordinate case aliases while exact-case root locks
remain compatible with older binaries. An older process still recognizes only
the path spelling it opened; cross-spelling coexistence requires both processes
to use the folded protocol.
Conflict cards name the file or workspace being written. Git is never required.
A finished conversation does not keep the write lease; use a worktree when you
need a long-lived isolated tree.
