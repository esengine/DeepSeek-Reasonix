# `use_capability` replay evaluation

This optional paired evaluation compares the cache-stable `use_capability`
proxy with a baseline that expands MCP tools into the provider-visible schema.
It is a diagnostic benchmark, not a Stable release gate. Live model runs are
optional and must use disposable Reasonix homes.

## What to measure

For the same task set, run each task twice:

1. **Proxy (default):** `use_capability` only. Shared Host plus disk schema
   cache.
2. **Baseline:** a throwaway config that still expands MCP tools into the
   provider request. Native Tool Search must stay off.

Record `tools/list` count, first-token latency, and cache-hit tokens. Do not
upload prompts, secrets, tool arguments, or workspace paths.

## Procedure

1. Use disposable `REASONIX_HOME` and `REASONIX_CACHE_HOME` directories.
2. Pick a representative task set that needs MCP discovery followed by a call.
3. Run proxy and baseline for each task with the same model, effort, workspace,
   skills, agents, and MCP configuration.
4. Record content-free pairs matching
   `internal/eval/replay/testdata/paired_runs.json`.
5. Exercise the median helper:

```bash
go test ./internal/eval/replay/ -run TestMedianReportFivePairedRuns
```

The repository fixture is synthetic and proves only the median helper. Teams
may replace its numbers with live observations for performance analysis, but no
paired-run dataset or threshold result is required for Stable publication.

Native first-party Tool Search stays default-off and is not part of this eval.
