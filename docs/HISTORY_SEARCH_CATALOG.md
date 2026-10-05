# History Search Catalog

Reasonix stores the history search projection at
`<cache root>/history-search/v1.sqlite`. Session JSONL, event logs, metadata,
sub-agent transcripts, and archives remain authoritative. Removing or
rebuilding the database never removes conversation data, and older Reasonix
versions continue to read the same authoritative files.

The catalog stores normalized retrieval tokens in FTS5, not complete message
text. English tokens retain the existing lowercase/code-symbol semantics and
CJK text retains overlapping bigrams. Snippets and `around` context are loaded
from the authoritative source only after SQLite has selected the final
candidates. User, assistant, tool input, tool error, and tool output parts are
indexed; ordinary tool output remains excluded by the default search kinds.

Successful session persistence sends a non-blocking, path-coalesced hint after
the authoritative commit and after file locks have been released. Continuous
appends read only the new display-index range. Rewrites, missed notifications,
external writers, or fingerprint mismatches rebuild that source in the
background. Only one full transcript decoder runs at once, checkpoints are
persisted per source, and one corrupt source cannot stop other histories.

The Agent `history` tool and Desktop history manager share the projection.
Search never starts a synchronous directory scan. While indexing is incomplete,
existing results return immediately with explicit progress. Runtime open,
running, and current state is overlaid from memory; SQLite cannot restore stale
runtime ownership. Provider-visible tool name, description, schema, defaults,
and ordering are unchanged.

The database uses the common disposable projection policy: local disks use WAL,
`synchronous=NORMAL`, foreign keys, private permissions, and a short busy
timeout. Remote or unavailable cache directories fall back to memory. Integrity
or migration failures quarantine and rebuild the cache; future schema versions
are preserved in degraded read mode.

Diagnostics and safe rebuild commands:

```sh
reasonix doctor catalogs [--json]
reasonix catalogs reindex history [--dir PATH ...] [--json]
```

Diagnostics never print queries, tokens, snippets, messages, tool arguments, or
provider content. A rebuild replaces only this disposable projection.
