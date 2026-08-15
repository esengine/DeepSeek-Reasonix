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

Delivery worktrees stay optional. Non-isolated directories use the workspace
lease (`filelock`). Conflict cards can recommend an existing worktree. Git is
never required; Windows without Git still serializes writers through the
workspace lease.
