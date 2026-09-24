# Performance diagnostics (debugpprof)

<a href="./PERFORMANCE_DIAGNOSTICS.zh-CN.md">简体中文</a>
&nbsp;·&nbsp;
<a href="./DESKTOP_CRASH_DIAGNOSTICS_RUNBOOK.md">Desktop crash diagnostics</a>
&nbsp;·&nbsp;
<a href="./CAPABILITY_DIAGNOSTICS.md">Capability diagnostics</a>

The Go service carries an optional diagnostics build that records phase timings
and attribution counters for the desktop catalog, sidebar, host RPC, workspace
state, and persistent-shell paths. It is gated behind a compile-time build tag:
ordinary builds link no-op stubs and pay nothing.

## Why direct instrumentation

A sampled CPU profile on Windows charges samples to threads blocked in syscalls,
so I/O-bound phases (catalog scans, sidebar projections, registry loads) show up
as "CPU" they never used. Those phases are therefore timed directly, and slow
host RPCs are logged with their method name, so startup hydration is attributed
to a concrete call rather than to a sample.

## Build and run

```bash
cd desktop
go build -tags debugpprof -o reasonix-desktop-debug.exe .
```

Without the tag, `debugTickPhase` / `debugNote` (desktop), `debugPhase` /
`debugSkipped` (sessioncatalog), `debugRPCTiming` (hostrpc), `debugConflict` /
`deepestConflictFrame` (workspacestate), and `debugPersistentFallback` (builtin)
all compile to no-ops.

## Where the output goes

Under the tag, `slog` is mirrored to **stderr** and to:

```
<desktopConfigDir>/logs/desktop-debug.log
```

The file rotates to `desktop-debug.log.1` once it exceeds 8 MB. The desktop
launcher does not capture the service's stderr, so this file is the only durable
place a local run's timings survive.

## Signals

| Signal | Log line | Covers |
| --- | --- | --- |
| Phase timing | `debugpprof: tick phase phase=… took=…` | catalog and sidebar phases (below) |
| Slow host RPC | `debugpprof: slow rpc method=… took=…` | every `Registry.Invoke` call ≥ 50 ms |
| Mutation conflict | `debugpprof: workspace mutation conflict site=… …` | 11 raise sites, plus the guard frame |
| Catalog scan | `debugpprof: catalog phase phase=signature\|scan target=… took=…` | per-directory reconcile |
| Catalog skip | `debugpprof: catalog scan skipped (signature unchanged) target=…` | directories whose signature is unchanged |
| Shell fallback | `debugpprof: persistent shell fallback shell=… cause=…` | demotion to isolated execution |

### Phase timings

| Group | Phase name |
| --- | --- |
| Snapshot RPC | `snapshot-rpc`, `snapshot:projects-file`, `snapshot:project-shells` |
| Topic listing | `list-topics:availability:<root>`, `list-topics:catalog-page:<root>`, `list-topics:merge-metadata:<root>`, `list-topics:live:<root>` |
| Catalog page | `catalog-page:preferred:<root>`, `catalog-page:list-topics:<root>` |
| Sidebar legacy | `legacy:<root>`, `merge-shells`, `legacy-page:<root>` |

### Workspace mutation conflicts

`debugConflict` records a persisted-vs-expected divergence together with the site
that raised it. `deepestConflictFrame` appends the innermost `lifecycle.go` /
`store.go` frame as `raise=<file>:<line>`, so a guard inside `commitOperation` or
`PrepareOperationContent` is attributed to that guard rather than to the `Store`
method that called `mutate`.

## Live profiling

Set `REASONIX_PPROF=1` alongside the tag to expose `net/http/pprof` on
`127.0.0.1:6060` (CPU, heap, goroutine, block, and mutex profiles).

```bash
REASONIX_PPROF=1 ./reasonix-desktop-debug.exe
```

## Scope

This covers the Go service only. The Electron/renderer side is covered
separately by `.github/workflows/diagnostic-overhead.yml`; the Go service phases
above are the gap that build tag fills.

## Tests

The tagged build previously carried no tests. It now carries:

- `desktop/debug_diagnostics_test.go` — the no-op contract ordinary builds link against (`//go:build !debugpprof`).
- `desktop/debug_diagnostics_debug_test.go` — a compile gate that references the tagged `debugLogPath` (`//go:build debugpprof`).
- `desktop/internal/workspacestate/debug_ws_frame_test.go` — `deepestConflictFrame` never panics and reports `file:line` segments (untagged).

CI must build the tagged path as well as the default one:

```bash
go build ./... && go build -tags debugpprof ./...
cd desktop && go build ./... && go build -tags debugpprof ./...
```
