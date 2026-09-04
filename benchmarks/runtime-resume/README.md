# runtime-resume — the resurrection probe

One question, no fixes: **after the process that built a session exits, what can
a new process still prove about that session's runtime semantics without asking
a model?**

The measurement boundary is the OS process. Nil-ing an `Agent` or a `Session`
leaves package-level singletons, live sinks, registries and caches standing, any
of which can revive state that would not have survived a real restart. Each
phase here is a child process that exits before the next one starts.

```
process A (construct)          process B (resume)
  boot a real runtime            boot a real runtime
  drive turns (scripted)         agent.LoadSession(path)
  set a durable Goal             ctrl.Resume(session, path)
  fold the context               read host state
  record what it sees            record what it sees
  EXIT                           EXIT
```

Between them an arm may run a **lever**: a production-reachable change, never a
poked field.

## Running it

```bash
go run ./benchmarks/runtime-resume                      # all arms, temp roots
go run ./benchmarks/runtime-resume -only exact -keep    # one arm, keep the roots
go run ./benchmarks/runtime-resume -json matrix.json    # machine-readable too
```

No network and no key: the provider is a deterministic script in-process. Each
arm demands a clean root — a reused one puts a lever's effect into the construct
phase, which then reads as "the lever changed nothing".

## Phase B never calls a model

This is the point of the probe, not an optimisation. If phase B ran a turn, a
model could reconstruct a lost step id or goal from the transcript and the row
would go green while the host had in fact forgotten it. Phase B boots, loads,
inspects, and stops — so every PASS means *the host can prove this before a
provider call*.

## Arms

| Arm | Shape | Lever | Asks |
| --- | --- | --- | --- |
| `exact` | fold, then die | none | does anything change when nothing changes? |
| `append-after-fold` | fold, one more turn, die | none | same, with the projection no longer covering the whole transcript |
| `system-swap` | fold, one more turn, die | save a **pinned** memory fact | does a changed stable prefix invalidate a projection that would otherwise survive? |
| `covered-mutation` | fold, one more turn, die | rewrite a covered row via `SaveRewrite` | control: the digest's material changed, so it must not be reused |

The three lever arms all append after the fold on purpose. Without it the
projection is already gone at the boundary for an unrelated reason, and the
lever's own effect cannot be read out of the result.

`system-swap` pins its fact deliberately: only a **pinned body** rides the
stable prefix. The saved-fact index is retrieval-only and reached through the
`memory` tool, so a relevant-activation fact moves nothing.

## Verdicts

Not booleans. "The host holds it verbatim" and "a fold over canonical artifacts
happens to agree" are different durability claims, and a row that was never
established supports neither.

| Verdict | Means |
| --- | --- |
| `persisted-direct` | a named artifact holds the value; the resumed value equals it |
| `reconstructed-exact` | no verbatim artifact; re-derived from canonical state and equal |
| `reconstructed-lossy` | present after the boundary but different |
| `lost` | established before, absent after |
| `not-measured` | never established before — the row asks nothing |
| `changed` / `stable` | the arm's own control row, not a durability claim |

`not-measured` exists so the probe cannot commit the substitution the
benchmarks contract names: **not evaluated read as false**. A row the construct
phase failed to establish is not a defect, and the first run of this probe hit
exactly that — a rejected `todo_write` reported `not-measured`, not `lost`.

## Metric contract

```text
Metric:      ProjectionSurvivesProcessBoundary
Anchor:      Controller.ContextMaintenanceSnapshot().CheckpointState, read in
             process B after Resume and before any provider call
Population:  arms whose construct phase reached CheckpointState != "none" and
             whose control row matches what the arm set up
Excludes:    arms marked ARM INVALID; rows whose construct value is empty;
             any reading taken after a turn has run in phase B
Reads as:    whether the next request would ride the folded projection, not
             whether a projection is stored — the sidecar row answers that
             separately, and the two disagree
```

The two projection rows are deliberately separate. A sidecar that loads and a
projection the next request actually rides are different states, and conflating
them is how a stored-but-unused projection reads as healthy.

## What this does not measure

- **Open decisions.** `approvalManager` holds pending approvals and asks in
  memory; the probe never establishes one, so those rows report `not-measured`.
  Establishing one needs a gate that blocks, which a headless probe must then
  answer.
- **The run graph.** `publishGraph` emits `GraphDelta` to the event sink; the
  probe never runs a fan-out, so the graph rows report `not-measured`. The
  narrow question to answer next is not "should the graph be persisted" but
  "can process B rebuild process A's terminal graph from artifacts that already
  exist" — the session event log is a transcript log, not this event stream.
- **Anything that only happens at turn time**, because phase B deliberately
  does not take a turn. A resumed session's leading system row is the persisted
  one; whether a later turn swaps in the freshly composed prompt is outside
  this probe's boundary.
- One session shape, one workspace, one platform per run.
