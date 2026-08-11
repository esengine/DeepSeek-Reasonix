# Session Catalog and Desktop Startup

Reasonix keeps session transcripts, event logs, metadata sidecars, and
`desktop-projects.json` as the only authoritative session data. The desktop
project tree reads a disposable SQLite projection from
`<cache root>/session-catalog/v1.sqlite`; deleting that database never deletes
or changes a conversation.

## Invariants

- Startup and project-tree requests never decode transcript JSONL, run legacy
  migration, or wait for a directory scan.
- A successfully saved transcript is committed before its catalog update. The
  non-blocking writer coalesces updates by session path; background
  reconciliation repairs updates dropped under queue pressure.
- Missing legacy counts are represented as `unknown`. The session is visible
  immediately, then a single repair worker decodes it in the background.
- A missing file is marked degraded on the first scan. It is removed from the
  projection only after a second scan and the missing-file grace period.
- Runtime state (`open`, `running`, and live status) comes only from in-memory
  controllers and overlays catalog results. It is never persisted to SQLite.
- Catalog, migration, plugin, and MCP work is cancellable and never participates
  in the desktop shutdown lock. Shutdown gives pending catalog writes at most
  250 ms.

## Storage and migration

`internal/sessioncatalog` uses a version ledger in `schema_migrations`; database
existence is not a migration signal. Local cache files use WAL,
`synchronous=NORMAL`, and a short busy timeout. An unavailable or obviously
remote cache path falls back to an in-memory catalog so storage failures cannot
block the application.

At open, Reasonix runs an integrity check. A corrupt or unmigratable database is
renamed with a `.corrupt-<timestamp>` suffix and replaced. The replacement is
rebuilt from sidecars and transcripts in the background. The quarantine and
rebuild paths never remove authoritative files.

The catalog stores only query projections:

- directory signatures, scan generations, checkpoints, and errors;
- project ordering, title, color, and pin state;
- topic ordering, aggregate counts, activity, recovery, and health state; and
- session path, preview, counts, fingerprints, recovery, and health state.

Topic pages use a `(pinned, last_activity_at, topic_id)` keyset cursor. The
default page size is 50 and the maximum is 200. Directory reconciliation commits
at most 64 sidecars per batch and persists its checkpoint before yielding.

## Desktop API

- `GetProjectTreeSnapshot` returns project shells, catalog state, progress, and
  revision without opening a session or sidecar file.
- `ListProjectTopics` performs cursor-paged search and time filtering.
- `GetTopicSummary` resolves one topic for active-turn UI without rebuilding the
  tree.
- `GetSessionCatalogStatus` and `RebuildSessionCatalog` expose safe diagnostics
  and replacement.
- `project-tree:changed-v2` carries a monotonic revision, affected workspace
  roots, and reason. Clients ignore older revisions and refresh only expanded
  affected roots.

`ListProjectTree` remains as a compatibility wrapper over the catalog. It no
longer has a synchronous filesystem fallback.

## Operations

Inspect the catalog without creating or changing it:

```sh
reasonix doctor sessions
reasonix doctor sessions --json
```

Replace only the disposable projection and index all saved desktop projects:

```sh
reasonix sessions reindex
reasonix sessions reindex --json
```

Use repeated `--dir PATH` flags to rebuild from an explicit set of directories.
Explicit directories are treated as global scope. Reindexing never edits or
deletes transcript, event, metadata, recovery, archive, or project files.

## Plugin isolation

Manifest validation and plugin handshakes are independent from catalog and
project-tree work. An incompatible plugin is reported as
`disabled_incompatible`; the core controller remains usable. A legacy manifest
under Reasonix's managed plugin directory is atomically upgraded with a backup.
Development directories, absolute external roots, and symlinked sources are
never rewritten automatically and include a manual migration hint instead.

## Release gates

Preview/canary promotion should track catalog repair backlog, rebuild failures,
page latency, queue pressure, and shutdown duration. Required checks include
legacy/ corrupt fixtures, deterministic lifecycle races, `go test -race`, the
React contract tests, and `CGO_ENABLED=0` builds for supported macOS, Windows,
and Linux architectures.
