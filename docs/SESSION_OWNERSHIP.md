# Session ownership, rewind, and worktree fallback

How Reasonix decides who may write a session, how conflicts are saved, and
how rewind and workspace isolation interact.

## Session writers

A conversation lives in an append-only event log, `<id>.events.jsonl`
(session format 2). Every message entry carries its own id and the id of its
parent, so the log is a DAG: each path from a leaf back to the root is one
version of the conversation, called a *head*. A log starts with one head,
`main`; forks, rewinds, and concurrent writers add heads of kind `fork`,
`rewind`, and `concurrent`. The transcript file `<id>.jsonl` is a derived cache
of the selected head and is never the source of truth.

The selected head is the last `select` marker that still points at a live
head, otherwise the head with the newest activity. Opening a session by path
opens that head. `.event-index.json` mirrors the heads so the session catalog
can list them without replaying the log.

Writers append; they never rewrite or truncate the log. A save takes the
bounded `.jsonl.lock` flock, reads whatever another writer appended since its
own last save, and continues on its own head. The session lease
(`.lease.lock`, with a generation-bound `SessionWriter`) no longer gates
appending: it decides who writes the derived `.jsonl`, the indexes, and the
turn ledger, so a second window can join the same conversation without
waiting. A turn opens with a `turn_begin` marker and closes with `turn_end`;
a crash between them is noticed on the next open and the incomplete tail is
set aside with a `rewind` marker, never truncated. When the shutdown save
cannot take the save lock within its bounded wait, it appends the unsaved
tail without the lock on a fresh `concurrent` head and leaves the derived
files to the next locked save; a shutdown never writes a copy of the session.

Path changes (`new`, `clear`) still use the prepare-before-publish handoff:
the frontend acquires the target lease and binds the unpublished Session
before the controller swaps paths. `fork`, `branch`, `switch`, and
conversation rewind stay on the same path and move between heads instead.

Sessions saved before Reasonix 1.39.0 use format 1: a whole-file transcript
plus a position-based event log. A 1.39.0 or newer binary upgrades such a
session in place on its first save, once it can prove it is the only writer;
until then the session keeps the format-1 rules below. A binary older than
1.39.0 refuses to open a format-2 log and leaves the file untouched;
`reasonix doctor session <id> --export-v1 PATH.jsonl` writes the current head
back out as a format-1 session when a rollback needs it.

## Conflicts

Two processes appending to one format-2 log cannot conflict; they interleave.
When a save finds that another writer extended the chain this session was on
and this session added nothing, it follows the disk. When both added content,
the save forks a `concurrent` head from the last shared message and continues
there; both sides receive a notice (`session_concurrent_writer`). Reloading
the log afterwards opens the newest head and lists the other one under *View
versions* (`session_head_switched`). Nothing is copied into a `-recovery-`
file, and a save never removes a head.

Format-1 sessions keep the previous rules until they are upgraded:

1. Event-log tail still matches this writer → normal save (no-op / append / replace).
2. Disk already covers the local prefix → adopt disk, no branch.
3. True divergence, replaced log, or deleted original → one stable recovery
   file keyed by root branch ID + the live Session's first writer generation.
   Lease rebinds keep that lane; later conflicts update the same path. There is
   no recovery-on-recovery chain.

## Heads as versions

Fork-from-here, `/branch`, and a conversation rewind append a `fork` marker
and a `select` marker: the new head starts at the chosen message and becomes
current, and the previous chain stays as another version of the same
conversation. The desktop switches the current tab to the new head in place;
the terminal replays the transcript. *View versions* lists the live heads with
their kind, makes another head current (`select`), renames a head, and
removes *covered* heads: heads whose whole chain is already part of the
current one. Removing appends a `retire` marker; a retired head leaves the
version list, and its bytes are reclaimed only when a single writer rotates
the log. Heads with unique content are never removed automatically, and a head
that was active within the last minute is reported busy instead of retired.

## Rewind

- **Code**: restore file before-images. Already-restored files (current ==
  before) are skipped. External changes refuse overwrite.
- **Conversation**: fork a `rewind` head at the turn boundary and make it
  current. The previous chain is never truncated. A format-1 session forks a
  new session file instead.
- **Both**: fork first, then restore files. A file conflict keeps the new head
  and reports `partial=true`.
- **Undo**: restores the file after-images. If nothing was added on the rewind
  head since, the controller returns to the parent head and retires the empty
  rewind head; a continued rewind head stays as a version.

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
across tab close/restart. New allocations also store a mode-0600 v1
`metadata.json` beside the checkout. It binds the original source checkout,
target branch, creation `HEAD`, managed worktree root, and temporary branch.
Older allocations without this metadata cannot use Merge-Back because Reasonix
will not guess a destination; the UI leaves them intact and shows manual merge
guidance. Unknown metadata versions also fail closed.

Merge-Back is a two-phase, failure-atomic operation. Preflight verifies the
managed path and repository identity, exact branches and `HEAD`s, clean source,
absence of an in-progress Git operation, all visible or detached Desktop work,
integrated terminals, workspace write leases, divergence, diff, and conflicts.
After the dual leases are held, Desktop briefly quiesces turn starts and
controller publication, then reserves both canonical source and worktree roots
through the Git mutation. Project-runtime owners, new turns, and terminal
create/write calls all use that admission gate; contained paths and symlink
aliases are covered without blocking prefix siblings or unrelated workspaces.
Uncommitted worktree changes block the merge unless the user explicitly opts
into an automatic commit; that option is off by default. Confirmation binds a
transient, NUL-safe token to the real index entries and status as well as every
dirty path's type, mode, bytes, or symlink target. Auto-commit seeds a private
`0600` temporary index from the confirmed `HEAD` and runs `git add -A` only
there. If the real index contains staged or index-only content that the full
working tree does not represent, Reasonix stops with the real index and both
versions untouched. Otherwise it creates a hook-free, single-parent
`commit-tree`, compare-and-swaps only the confirmed worktree branch, and then
installs the prepared index through Git's exclusive `index.lock` protocol only
if the real index bytes still match. Any failure after the branch CAS is marked
recovery-required; conflict preflight runs again on the exact new commit. A
target branch, `HEAD`, index, or content-token change refreshes the confirmation
instead of continuing. The source merge uses
`git merge --no-ff --no-commit --no-verify` with a Reasonix-scoped committer
identity, so it neither depends on user Git identity nor invokes commit hooks.
It binds the real index tree to a freshly computed merge tree. The worktree root, common repository,
symbolic branch, branch ref, `HEAD`, Git operation, and content token are
revalidated before preparation and before ref installation. Only while those
identities, the target branch, original `HEAD`, exact `MERGE_HEAD`, and prepared
tree still match does Reasonix create a hook-free `commit-tree` object with
fixed parents and tree. A short source mutation fence holds the real index,
`HEAD`, and `MERGE_HEAD` lockfiles and compares their exact snapshots. While
those checkout-local locks remain held, Git uses a detached administrative view
of the same common ref store to acquire only the branch ref locks. One
`update-ref --stdin` transaction verifies the
worktree branch ref and compare-and-swaps the target ref against its original
`HEAD`, so neither ref check can partially succeed. Post-commit verification
rechecks both checkouts plus the commit tree, real index tree, parents, refs,
clean state, and Git operations. After installation, `git merge --quit` removes
only Git's auxiliary merge state; Reasonix does not update the `MERGE_HEAD`
pseudoref directly or reset the prepared index. Owned preparation failures
before the CAS are aborted only when the prepared state can still be proved;
target-ref drift, post-CAS drift, or any state whose recovery cannot be proven
is marked recovery-required while every worktree resource and external state
is preserved.

A successful merge first navigates to the recorded source checkout. Every UI
navigation registers an opaque intent token with Desktop; the close request
must still own that exact token both before its snapshot and at the backend
removal linearization point. A newer intent therefore stops close and cleanup
while preserving resources. Otherwise Desktop closes only the exact idle
worktree tab while the exact source tab is still active. Cleanup is then a
separate, retryable step. It reserves the complete allocation containing both
the canonical worktree and its fixed recovery subtree while checking visible
and detached runtimes; every project-runtime creation, restoration,
delete/archive fallback, and redirect uses the same gate. Symlink and contained
paths are covered. Prefix siblings outside the allocation and other allocations
remain independent.

Finalization runs only when the temporary commit is contained by the target,
identities still match, and the full status including ignored files is empty.
Before moving anything, Reasonix writes a mode-0600 v2 `cleanup-state.json`
journal with the original root, an unguessable recovery root under the reserved
allocation, branch, `HEAD`, and a `planned` stage. It then uses ordinary
`git worktree move` and rechecks the common Git directory, symbolic branch,
branch ref, `HEAD`, operation state, full status, and registered path before
advancing the journal to `retained`. A crash in either stage is retried from the
Git worktree registry and the exact journal identity; multiple or unknown
candidates fail closed.

The recovery checkout deliberately stays registered and keeps its
`reasonix/delivery-*` branch checked out. Reasonix does not unregister the
worktree, delete its branch, unlink manifest entries, or recursively delete any
path. An already-open file descriptor therefore follows the moved checkout and
late writes remain recoverable; content recreated at the former public path is
also left untouched and reported. Desktop removes only the stale managed-project
registration after the recovery receipt is durable, keeps the source project
active, and does not add the hidden recovery path to the sidebar. Registration
failure is retryable while the recovery root and journal remain available.

New readers accept v1 cleanup journals only for preservation. A still-registered
legacy checkout can be converted to v2 after its exact identity and manifest are
proved; a detached or ambiguous legacy root is reported for manual recovery and
is never deleted or automatically re-registered. Unknown journal versions fail
closed. Metadata remains v1; older cleanup readers reject the unknown v2
journal and therefore preserve the recovery checkout.

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
