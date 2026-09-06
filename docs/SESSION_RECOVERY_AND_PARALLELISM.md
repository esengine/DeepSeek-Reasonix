# Session Recovery and Parallel Work

Reasonix keeps transcript persistence and workspace mutation as separate safety
boundaries. Read-only work and non-overlapping file claims can run concurrently;
opaque writers such as unrestricted shell or unknown MCP mutations retain the
workspace write lease. Git worktrees provide an isolated checkout when a task
needs an independent workspace.

## Session versions

Each physical JSONL transcript has a version identity in its branch metadata:

- `normal` is an ordinary conversation transcript.
- `recovery` preserves local content after a transcript save conflict, file-lock
  timeout, or external removal.
- `subagent` is reserved for a session-backed child run.

Older sidecars remain readable. A sidecar with `Recovered=true` is interpreted as
a recovery version when the explicit version field is absent.

Recovery metadata records the parent conversation/version and the base and disk
revisions observed at the conflict. Recovery copies stay in the same logical
conversation lineage and are not treated as ordinary conversations or
subagents.

## Recovery lifecycle

An append-compatible snapshot is adopted from disk without creating another
version. A divergent snapshot is preserved as a recovery version using the
existing CAS and digest checks. A failed lease handoff marks that recovery
version as `pending`; the desktop client can retry activation after the writer
is released.

Recovery lineage reconciliation is idempotent. Covered copies may be moved to
recoverable session trash, while divergent content remains available for an
explicit version choice.

The desktop bridge exposes `GetSessionVersionState`,
`SetActiveSessionVersion`, `RetrySessionRecovery`, and
`ReconcileRecoveryVersions`. Worktree status and merge preparation use the
same backend inspection and identity checks as the existing merge flow.
