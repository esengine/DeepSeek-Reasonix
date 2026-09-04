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
| `refold-into-body` | fold, grow, fold again, die | none | can a second fold, which reads the projected view, record its boundary without losing what it kept? |
| `tail-edit` | fold, one more turn, die | `SaveRewrite` edits a row past the fold | a change the covered hash does not cover |
| `tail-truncate` | fold, one more turn, die | `SaveRewrite` drops everything past the fold | the same, by removal |
| `tail-rewind` | fold, one more turn, rewind, die | none | the same truncation through the rewind a person drives |
| `covered-rewind` | fold, one more turn, rewind below the fold, die | none | negative control for making that rewind conditional |
| `todo-identity` | identity A, fold, grow, identity B, grow, fold again, die | none | does the host's step-identity note stay single, current, and on the tail? |

The three lever arms all append after the fold on purpose. Without it the
projection is already gone at the boundary for an unrelated reason, and the
lever's own effect cannot be read out of the result.

`refold-into-body` is the discovery arm rather than a red one. The second fold
reads the provider-visible view — stored body plus live tail — and has to write
a boundary back in canonical terms, which the body has no counterpart for. What
it can lose is the part of the body past the new boundary, so the arm carries an
oracle rather than a comparison.

The tail arms exist as a pair on purpose. A tail change reaching the
transcript and the same change driven through `Controller.Rewind` are different
paths, and only one of them consults coverage; separating them is what says
whether a red belongs to the validity rule or to the caller that discards the
projection before the rule is asked.

`covered-rewind` targets a middle checkpoint rather than the first. Rewinding
to the first empties the conversation, and a session holding only a system row
is not persisted at all, so the restart would read back the transcript the arm
had removed — a different rule answering instead of the one under test.

`system-swap` pins its fact deliberately: only a **pinned body** rides the
stable prefix. The saved-fact index is retrieval-only and reached through the
`memory` tool, so a relevant-activation fact moves nothing.

## The marker oracle

Every turn is tagged twice: `PROBE-MARK-nnn` in the user prompt and
`PROBE-ECHO-nnn` in the reply. A fold takes assistant work first while the
retention budget keeps user turns verbatim, so a one-sided tag lets a surviving
user turn hide the reply dropped beside it — the first version of this arm
reported "1-11 intact" for a fold that had removed four replies.

The judgement runs on the assistant side, and the shape a fold can produce is
narrow: an optional pinned head, then one run of replies reaching the newest
turn. Two gaps, or a run that stops short of the newest turn, is a loss no fold
made. The scripted digest contains no tag, so a summary can never stand in for
a message that should have survived verbatim.

`refold_test.go` holds the oracle to that: a test that cannot fail proves
nothing, so the shapes only a loss can produce are asserted to be caught.

## The todo identity gate

`todoIdentityProjection` calls itself a turn-tail note owed when the host holds
step ids the conversation no longer shows. Once the frozen body stopped holding
the live tail, that claim became checkable, and the arm checks all three parts
of it rather than whether a note exists: exactly one note reaches the model, it
rides the tail, and it names the ids the host holds now.

The scenario moves identity while a fold sits between the model and it — write
a list, fold, grow, rewrite the list, grow again, fold again — because one run
then separates placement from duplication from staleness. The rewrite keeps the
in_progress item and replaces the pending work: the host refuses to drop an
item in flight, so that is what a list rewrite actually looks like.

The second growth is not padding. A fold that leaves the new ids readable asks
the note for nothing, and the arm would pass without ever exercising it — so
the gate reports whether the ids were still readable, and expects no note when
they were. Otherwise the migration could turn into an unconditional append and
every row would still be green.

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
| `contiguous` / `HOLE` | the shape of surviving work in the view, for a fold |
| `holds` / `VIOLATED` | a contract the arm asserts outright, not a comparison |

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

## Provenance

Studio independently converged on the durable covered-identity contract also
present in main-v2 `f60ab17b0` (#8923); that commit is not in studio's
ancestry. The same commit carries two further contracts studio has not adopted
— live-tail ownership, and wire-safe fingerprint compatibility — which is why
that line is ported by contract rather than by file.
