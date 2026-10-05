# Usage Catalog

Reasonix stores disposable usage rollups at `<cache root>/usage-catalog/v1.sqlite`.
Daily statistics JSONL remains authoritative and byte-compatible with older
versions. The catalog records file offsets and line hashes for cross-process
idempotency, then derives daily source/model/provider rollups.

Only a successful JSONL append emits a non-blocking receipt. Duplicate receipts
cannot double-count a line. A changed hash, truncated file, replacement, queue
overflow, or external append triggers byte-offset reconciliation or a safe
per-day rebuild. Torn tails, blank lines, corrupt JSON, legacy request defaults,
time zones, date boundaries, and active-day semantics match the existing JSONL
decoder.

SQL aggregation is used only when every selected day is complete. Otherwise the
existing exact JSONL query runs on explicit statistics-page access while the
background index catches up. Catalog failures never block provider events,
turn completion, controller startup, or shutdown.

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex usage [--json]
```

Diagnostics expose schema, integrity, lag, counts, and failures without model
request content. Reindexing never changes daily JSONL.
