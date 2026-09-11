# Task Catalog

Reasonix stores the cross-project task projection at
`<cache root>/task-catalog/v1.sqlite`. Task snapshots, event JSONL, idempotency
files, file locks, version CAS, and leases remain authoritative. SQLite is never
used to accept stop, cancel, requeue, or open-session commands.

An observed `FileStore` sends non-blocking snapshot and event hints only after
the per-task lock is released. Snapshots are indexed eagerly in bounded batches;
event logs are indexed lazily when a task expands, an event query arrives, or a
new event is appended. Byte-offset and sequence checkpoints resume after an
interruption. Corrupt snapshots and event lines degrade only that task.

The Desktop task center pages across current session, project, or all registered
projects. Project keys are full SHA-256 hashes of canonical saved roots and are
resolved before every action. The control service then re-reads the authoritative
snapshot. Current process jobs and controller state always overlay catalog data,
and expired leases are reconciled at read time without rewriting snapshots.

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex tasks [--project PATH ...] [--json]
```

Catalog corruption or partial indexing does not disable task control. Reindexing
replaces only the disposable projection.
