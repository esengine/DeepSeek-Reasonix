# Extension Runtime v2 — Performance baselines

Soft CI thresholds live in `internal/extension/bench_threshold_test.go`.
Benchmarks live in `internal/extension/benchmark_test.go`.

## Targets (developer machine / CI soft fail)

| Operation | N | Soft upper bound |
| --- | --- | --- |
| `BuildDependencyGraph` | 32 components | < 50ms |
| `DiffRuntimePlan` no-op | same graph | < 20ms |
| `EffectScope.Dispose` | 64 effects | < 50ms |

## Incremental vs full rebuild

- **No-op / interceptor / UI / provider / MCP-only** (`RebuildFrom` + true subgraph patch): must **not** call `BuildRuntime`. Metrics: `NoOpRebuilds` / `SubgraphRebuilds`.
- **Full** (`SubgraphSidecar` / `SubgraphFull`): `FullRebuilds` + full `BuildRuntime`.

Measure:

```bash
go test ./internal/extension/ -run 'TestGraphAndPlanLatencyBaseline|TestEffectScopeDisposeBaseline' -count=1
go test ./internal/extension/ -bench 'BenchmarkDependencyGraphAndPlan|BenchmarkExtensionKernelStartup' -benchmem -count=3
go test ./internal/boot/ -run 'TestIntegrationNoOpDoesNotBuildNewController|TestRebuildFromNoOp' -count=1
```

## Cache hit expectation

No-op, UI/interceptor-only, and backend-only Provider/MCP plans keep the frozen
system prompt, tool schemas, and `RuntimeSnapshot.CacheHash` byte-stable.
Provider capability changes remain visible through `providerChanged` without
falsely reporting `prefixChanged`. MCP schema additions, removals, or renames
are classified as full rebuilds and intentionally recompute the prefix.
Discovery of skills/commands/hooks is skipped while `ReuseAssembly` is retained.

## Sidecar start / drain

`StartPackagesWithPlan` adopts Unchanged clients (`SidecarAdopts` metric) and
only starts Added/Reloaded. Drain uses `Manager.DrainPlan` after publish.
Drain TTL defaults to 30s; force-expire fires registered cancel callbacks then
writes `drain-timeout-<gen>` receipts.
Cold publishes allocate no watcher. While drains exist, rapid publishes share
one timer watcher per runtime owner; the watcher sleeps only until the oldest
drain reaches its TTL. Expired-generation markers are capped at 256 per owner.

Receipt evidence is process-local and bounded to 32 generations with 256
receipts per generation. Retention truncation is conservative: it prevents a
clean-rollback claim instead of hiding missing evidence.
Message dedup keys are removed with their matching evicted receipts. File-prior
retention is bounded to 8 MiB per write and 32 MiB per runtime owner; an
oversized prior is not retained and blocks a clean-rollback claim.
Completed provider streams remove their drain callbacks immediately, so a
long-lived generation retains only active stream cancellation state.
