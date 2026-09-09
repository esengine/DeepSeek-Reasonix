# Session Recovery and Parallel Work

Reasonix keeps transcript persistence and workspace mutation as separate safety
boundaries. Read-only work and non-overlapping file claims can run concurrently;
opaque writers such as unrestricted shell or unknown MCP mutations retain the
workspace write lease. Git worktrees provide an isolated checkout when a task
needs an independent workspace.

## Session versions

A format-2 session log holds every version of one conversation as a head of
its append-only DAG (see [`SESSION_OWNERSHIP.md`](SESSION_OWNERSHIP.md)).
Heads have kinds:

- `main` is the line the log started with.
- `fork` and `rewind` come from user actions: fork-from-here, `/branch`, and
  conversation rewind.
- `concurrent` is created by a save that found another writer on the same
  chain.

One head is *selected*; opening the session opens it. Heads are listed,
switched, renamed, and retired inside the log. Nothing creates a second
session file, and a head that was retired keeps its bytes until a single writer
rotates the log.

Format-1 transcripts (saved before Reasonix 1.39.0 and not yet upgraded) keep
their version identity in branch metadata:

- `normal` is an ordinary conversation transcript.
- `recovery` preserves local content after a transcript save conflict, file-lock
  timeout, or external removal.
- `subagent` is reserved for a session-backed child run.

Older sidecars remain readable. A sidecar with `Recovered=true` is interpreted as
a recovery version when the explicit version field is absent. Recovery metadata
records the parent conversation/version and the base and disk revisions
observed at the conflict; recovery copies stay in the same logical conversation
lineage and are not treated as ordinary conversations or subagents.

## Recovery lifecycle

A format-2 save never conflicts. Another writer's appends are followed when
this session added nothing; when both added content the save forks a
`concurrent` head, and both sides receive a notice. The versions dialog shows
both heads and the user picks. There is no `pending` state and no lease
handoff to retry: the lease decides only who writes the derived transcript,
the indexes, and the turn ledger.

For a format-1 session an append-compatible snapshot is adopted from disk
without creating another version. A divergent snapshot is preserved as a
recovery version using the existing CAS and digest checks. A failed lease
handoff marks that recovery version as `pending`; the desktop client can retry
activation after the writer is released. Recovery lineage reconciliation is
idempotent. Covered copies may be moved to recoverable session trash, while
divergent content remains available for an explicit version choice.

The desktop bridge exposes `GetRecoveryLineage` and `GetSessionVersionState`,
which list a format-2 log's heads (state `heads`; every member shares the log's
path and carries `headId`, `headKind`, `headName`, and `selected`);
`ChooseRecoveryBranch` and `SetActiveSessionVersion`, which take `headId` and
make a head current (an open tab switches in place, a closed session gets a
`select` marker); `CleanRecoveryLineage`, which retires covered heads and
reports heads active within the last minute as busy; `RenameSessionHead`;
and the format-1 `RetrySessionRecovery` and `ReconcileRecoveryVersions`. A
family whose format-1 root was upgraded after recovery copies had been made
shows the log's heads and the copies together. Worktree status and merge
preparation use the same backend inspection and identity checks as the
existing merge flow.

## Legacy recovery copies

`-recovery-` files made before the upgrade are not imported into the log. They
remain format-1 sessions of the same lineage: listed under *View versions*,
selectable, and covered copies are still moved to recoverable trash by the
existing sweep and by `reasonix sessions cleanup`. A format-2 log never forms
a recovery group, so cleanup reports zero candidates for it, and
`reasonix sessions diagnose` counts session logs, heads, covered heads, and
retired heads next to the recovery-copy numbers.

## Compatibility

| Field or format | Old-data behavior | New-reader behavior | Previous-reader behavior | Conclusion |
| --- | --- | --- | --- | --- |
| `.events.jsonl` format 1 | unchanged | replayed; message ids derived deterministically | unchanged | compatible |
| `.events.jsonl` format 2 | n/a | native | refused; file left untouched | explicit boundary (>= 1.39.0) |
| `.jsonl` transcript | unchanged format | derived from the selected head | readable, not authoritative | compatible |
| `.jsonl.meta` head fields | absent → treated as format 1 | used | ignored | compatible |
| `.event-index.json` schema 2 | schema-1 index rejected → replay | native | rejected → slow path | compatible |
| `-recovery-<hex>.jsonl` | format-1 lineage | not imported; listed as before | unchanged | compatible |
| session catalog `v8.sqlite` | v7 file isolated | native | separate generation files | compatible |

Format 2 opens in Reasonix 1.39.0 and newer. Mixed installations should
upgrade the older side before sharing a session directory; the older binary
reports the newer format and does not modify the file.
