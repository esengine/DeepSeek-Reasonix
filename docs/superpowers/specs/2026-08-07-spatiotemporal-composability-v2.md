# Spatiotemporal Composability (Plugin/Runtime v2)

## Context

Reasonix already has a solid extension kernel: `Contribution → RuntimeSnapshot`,
`RuntimeSet` reverse/idempotent cleanup with generation guards, Builder freeze
and activate stages, and `Rebuild` that keeps the old controller on failure.

This design ports four established runtime-composition mechanisms into that
kernel without adopting an external runtime or changing the implementation
language:

1. Unified runtime effect scopes
2. Typed, reactive dependency graph
3. Generation / epoch component lifecycle
4. Verifiable atomic reload (build → activate → publish → drain)

## Fixed decisions

- Upgrade native plugins to `reasonix.io/plugin/v2`
- Reject v1 and legacy native manifests (no apiVersion) at install, doctor, boot
- Keep existing stdio/HTTP transport; do not rewrite the transport layer
- Do not introduce an external runtime dependency
- Irreversible external work uses effect receipts; never fake rollback
- Receipt recovery is process-local, bounded, and does not promise crash recovery
- Preserve system prompt, tool schema, and memory prefix cache-first constraints
- Claude compatibility plugin paths are out of scope for v2 native runtime changes

## Lifecycle

Component states:

```text
Inactive → Preparing → Active → Draining → Inactive
Failed (activation or cleanup failure; retains error + receipts)
```

Component granularity for v1 of this design: one native v2 sidecar runtime
package is one component node. Host-owned providers, interceptors, and UI
bindings are child contributions of that node. Individual tools are not
independent lifecycle nodes yet.

## EffectScope

`RuntimeSnapshot` stays immutable configuration. Live handles live only in an
`EffectScope` (default implementation held by `RuntimeSet`):

| Class | Meaning |
| --- | --- |
| Reversible | Dispose undoes the effect |
| Cancelable | Cancel stops further work; wait for completion on dispose |
| Compensatable | Dispose may run a compensate function |
| Irreversible | Record receipt only; never claim rollback success |

Rules:

- Each effect runs dispose at most once
- Within a component: reverse registration order
- Across components: reverse topological drain order
- Independent components may activate/clean in parallel
- Mid-activation failure auto-disposes already tracked effects
- Dispose waits for cancelable background work
- Cleanup errors become diagnostics; they never skip remaining dispose

Resources that must track through EffectScope: sidecar process, MCP
client/transport, provider stream, watcher, event subscription, UI hub binding,
goroutine/background job, session temporary resources, controller cleanup
callback.

## Snapshot / plan / controller boundary

```text
RuntimeSnapshot = immutable configuration and dependency view
RuntimeSet      = live resources owned by one generation (EffectScope)
RuntimePlan     = transition from old snapshot to new snapshot
Controller      = owner of the published generation
```

Live handles (process, connection, watcher, callback, goroutine) must never
enter `RuntimeSnapshot`.

## Capability contract

`internal/extensioncontract` is a leaf package shared by `pluginpkg` and
`internal/extension`:

- `CapabilityKey{Namespace, Kind, ID}` — always namespaced
- `Capability{Key, Version, SchemaHash}` — stable schema hash required for
  provider/tool/UI capabilities
- `Requirement` extends capability with `VersionRange` and `Optional`

Versions use `golang.org/x/mod/semver`. Key, kind, version, and schema hash
participate in canonical hashing.

## Manifest v2

Native `reasonix-plugin.json` requires:

```json
{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "example",
  "version": "2.0.0",
  "requires": [...],
  "provides": [...],
  "runtime": { "command": "...", "intercepts": [], "replaces": [], "capabilities": [] }
}
```

Validation:

- Missing / v1 / unknown major apiVersion → hard reject
- `provides` is the capability ceiling for handshake
- Handshake must not declare undeclared capabilities
- Declared-but-missing capabilities become `Unavailable` (no forge)
- Non-optional missing requirements keep component `Inactive`
- Optional missing requirements allow activation with diagnostics
- Required dependency cycles fail with full cycle path
- Multiple providers for the same `(namespace, kind, id)` need explicit
  selection or report conflict
- Replacement slots stay single-owner; interceptors stay additive by priority

Claude/Codex compatibility manifests are unchanged.

## Dependency graph and RuntimePlan

Build flow:

```text
Discover → Parse → Validate → Resolve capabilities → Validate versions/schema
  → Detect cycles → Topological activate order → Reverse drain order
```

Deterministic sort keys: dependency rank, scope rank, priority, canonical
component ID.

Epoch identity for a consumer:

```text
epoch = [capability key, provider component ID, provider version, provider schema hash]
```

Only epoch changes force consumer reload.

`RuntimePlan` carries Added / Removed / Reloaded / Unchanged plus ActivateOrder
and DrainOrder. Diagnostics record `PrefixChanged` only after comparing the old
and new snapshot `CacheHash`, while `ProviderChanged` records provider capability
add/remove/reload independently. No-op plans must not change `CacheHash`.
Changes should affect only the relevant subgraph (provider, MCP server,
interceptor chain, UI hub).

## Atomic publish / drain

```text
Discover → ResolveGraph → CreatePlan → Preflight
  → ActivateNewGeneration → AwaitReady → PublishController → DrainOldGeneration
```

- Preflight failure: old runtime untouched
- New activation failure: dispose entire new generation scope
- Do not publish until new runtime is Active
- Publish is a single atomic pointer swap
- After publish: old controller Draining (no new turns; finish in-flight)
- Drain timeout cancels remainder and records receipts
- Stale generation UI / provider stream / event output is silently dropped

## Effect receipts

Irreversible and compensatable effects record owner, generation, component,
class, timestamps, receipt id, and compensation status. Provider requests
already submitted must not be reported as rolled back. File writes need prior
state or compensate. Sent messages get receipts and duplicate-send protection.
Cancellation stops later work only.

The ledger retains at most 32 generations and 256 receipts per generation.
Eviction marks recovery evidence incomplete, releases associated file priors,
and prevents a clean-rollback claim. The ledger is not persisted; process-crash
recovery is explicitly outside this design's scope.

## Permissions

The dependency graph answers what capabilities a component may obtain. It does
not replace the sandbox: trusted host components, native sidecars (process
boundary), and untrusted plugins still face permission interceptors at call
time.

## Error reasons (extension protocol)

In addition to the existing frozen table, v2 adds:

- `dependency_unsatisfied`
- `dependency_cycle`
- `schema_mismatch`
- `activation_failed`
- `stale_generation`
- `cleanup_failed`

(`unsupported_version` already exists.)

## Migration

- `reasonix plugin migrate <name> --to-v2` rewrites only pre-extension native
  manifests that omit `apiVersion`, backs up the original, and errors on
  dependencies it cannot infer. It does not accept Manifest v1.
- `reasonix plugin doctor` reports dependency and protocol errors
- v1 / missing apiVersion native manifests fail install, doctor, and boot

## Acceptance

1. Every v2 component resource has a clear owner and EffectScope
2. Activation failure never leaks new-generation resources
3. Missing dependencies become diagnostic Inactive, never panics
4. Dependency replacement reloads only the affected subgraph
5. Publish/drain order is testable
6. Irreversible work is never marked rollback-success
7. v1 native manifests are rejected without dual-read or migration
8. Prompt/tool cache stability guards pass
9. Doctor explains why a component is inactive
10. No external runtime or language dependency

## Implementation status (repo)

| Area | Status |
| --- | --- |
| EffectScope / LiveScope / RuntimeSet | Done |
| extensioncontract + DependencyGraph + RuntimePlan | Done |
| Manifest `reasonix.io/plugin/v2` + reject v1 | Done |
| Extension Protocol major v2 + schema/SDK gen | Done |
| PublishGate + stale UI/provider chunk drop | Done |
| Sidecar incremental adopt (StartPackagesWithPlan) | Done |
| RebuildFrom + desktop/CLI lastBuildResult | Done |
| MCP Host ReplaceServerBackend stable proxy | Done |
| Provider liveClient re-resolve | Done |
| LifecycleRegistry + FormatRuntimeStatus | Done |
| Manifest provides ceiling on handshake | Done |
| Full subgraph-only BuildRuntime (skip tools/prompt) | Done: plan classify + ReuseAssembly rediscovery skip + CacheHash reuse |
| Controller admission on PublishGate | Done (`turnDroppedDraining`) |
| Lifecycle metrics + graph/plan benchmarks | Done (soft CI baselines) |
| EffectScope for every MCP/watcher/UI resource | Done (activator + session-resources fold + Track* helpers) |
| Receipt-driven recovery / checkpoint | Done (`DecideResume` + doctor/desktop + provider-submit receipts) |
| Drain timeout product path | Done (`ScheduleDrainWatch` / `SweepAndForceExpire`) |
| Doctor runtime + desktop RuntimeDoctor | Done |
| Integration matrix (rapid reload, provider classify, admission) | Done |
