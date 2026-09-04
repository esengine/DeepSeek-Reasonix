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
| `open-decision` | ask a question, park the turn on it, kill the process | none | what does a restart inherit from a turn that died mid-decision? |
| `interrupted-idle` | that, then a restart that does nothing | none | does the interruption stay active with no work to settle it? |
| `interrupted-unrelated` | that, then a turn about something else | none | does it stick to requests that have nothing to do with it? |
| `interrupted-answered` | that, then a turn that reads like an answer to it | none | does context carry without execution continuing? |
| `graph-completed` | a fan-out whose items all settled, then die | none | is a finished fan-out's provenance durable at all? — and does a settled item stay settled? |
| `graph-running` | a fan-out with one item still executing, then die | none | what does a restart believe about work whose owner is gone? |
| `graph-mixed` | completed, failed, adopted and running at once, then die | none | the same, with every reachable state and both grants in one death |
| `wait-slots` | fill the total ceiling, refuse one more, die | ceilings, via project config | can a restart say which ceiling refused it? |
| `wait-writers` | fill the writer ceiling with total capacity free, die | ceilings | the same, one check further down |
| `wait-claim` | overlap two write paths with both ceilings free, die | ceilings | the same, at the last check |
| `wait-transition` | refuse on slots, then free the slots, die | ceilings | does a reported cause track the blocker, or the moment it queued? |

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

## The open-decision gate

This one does not ask whether a particular card comes back. "The same question
must survive" is a design requirement nobody has established: the goroutine
parked on it did not survive, so a restored card with nothing behind it would
be a durable-looking state that cannot be answered. The arm classifies instead:
**resumable** (the question is there to answer), **interrupted-explicit** (it is
gone and the host records that a turn was cut), or **LOST-SILENT** (gone, with
nothing saying it existed). Only the last is a defect.

One row is not a classification. The scripted round asks *and* tries to write in
the same reply; calling ask ends the round, so the write is the effect the
decision is holding back. It must not run while the answer is pending and must
not run because a later process loaded the session. Whether the question
survives is a design choice; whether what it blocked stayed blocked is not.

The process exits rather than shutting down: a clean close resolves or cancels
the question, which is the one thing a process dying mid-decision does not do.

## The successor arms

Three arms share `open-decision`'s ending and differ only in what the next
process does. They need a middle phase that runs a model — nothing else can
represent a person carrying on — so the discipline shifts rather than bends:
the phase that judges what the host knows is still the model-free one after it.

The `interrupted-answered` arm is the end-to-end reading worth naming. A turn
that reads like an answer to the dead question gets the interruption as
context, and the write that question was holding back still does not run.
Context continuity and execution continuation are different things, and this is
where the difference is measured rather than argued.

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

- **Whether a restored question could be answered.** The `open-decision` arm
  can see that a question is gone; it cannot tell a restored one that works
  from a restored one whose owner died with the process. Answering that needs a
  phase that tries to answer, which this probe deliberately does not have.
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

## The fan-out arms

Three arms ask the graph the question the decision arm asked the approval gate:
after the live owner dies, what does durable truth still hold? They classify
rather than pass or fail, and the labels are not a scale — **reconstructed-exact**,
**interrupted-explicit** and **reconstructed-lossy** are designs a host may
legitimately hold, while **LOST-SILENT** (gone, with nothing recording that it
existed) and **GHOST-RUNNING** (still drawn as running with no owner alive) are
defects. Ghost outranks every reconstruction: a fan-out that came back complete
and still says running is the worst of the five, not the best, for the same
reason a restored decision card with nothing behind it is worse than a dropped
one.

Two populations are classified separately, because one label over both reports
the better half. The items that had already settled own durable child records;
the item still executing has never been written down anywhere.

All three arms die inside the turn. The completed arm is not the exception: it
waits until every worker is terminal and then exits the same way, so what
separates the arms is which states the graph holds at the moment of death and
nothing else. Letting one arm shut down cleanly would have made it the only arm
whose turn was appended, and the comparison would have been reading that
instead.

`skipped` is missing from the mixed arm on purpose. A fan-out publishes an
item's skip only in its closing outcome delta, so no node reads skipped until
the group has ended — by which time nothing is running, and "skipped beside
running" is not a state a live fan-out can hold. The item whose dependency
already failed sits at `pending` instead, which is what the arm dies holding.

These arms run under the `auto` approval gate rather than the denied one every
other arm uses. `fleet` is not read-only, so a denied gate refuses the dispatch
before any child starts and the arm would measure a permission decision instead
of a process boundary.

## The invariant execution persistence must hold

> **Persisting execution existence must never imply resumability.**

A durable record saying that child B existed, held grant G, and was live when
its owner disappeared says exactly that. It does not say restart B, re-run its
tool calls, or continue its goroutine. This is the same shape as the decision
arm's finding — context continuity is not execution continuation — reached
independently: the moment execution facts become durable, the cheap next step
is to replay them, and that turns today's `LOST-SILENT` into the one outcome
this matrix ranks as worse than losing the graph.

The ghost row is where that invariant is enforced. It was green before anything
was stored, which is safety by absence rather than by design; it has to stay
green now that a journal exists, and any persistence that makes it red has
replayed something it should only have recorded.

## What the execution journal moved, and what it did not

The in-flight item was `LOST-SILENT` because a turn is appended when it ends:
nothing on disk had heard of the fan-out. `<stem>.execution.jsonl` is written
before the dispatch becomes observable, so that item now reads
`interrupted-explicit`. Two rows report it — what the journal recorded, and what
the next request is told about it — and the second one is a contract, not a
description: one block while an interruption stands, none otherwise.

Deliberately unmoved: settled items stay `reconstructed-lossy`. Their answers
already live in the sub-agent store, and a second durable copy of "this child
completed" would be a second authority to reconcile the moment the two files
disagree. The journal records that a delegation entered the orchestration and
that the orchestration let go of it, never how it ended.

**An execution record proves that work entered orchestration; it does not prove
that the work is resumable.** Whether it *started* is a separate durable fact,
recorded at the slot grant — before anything can observe the item running and
before the child can act. A restart therefore inherits two kinds of
interruption rather than one:

| Journal | Restart reads |
| --- | --- |
| opened, never started, not settled, no owner | `interrupted-before-start` — it never reached a slot, so nothing it would have done was done |
| opened, started, not settled, no owner | `interrupted-during-execution` — whatever it had written stands, whatever it had not is undone |

Neither is resumable, and neither is ever written down: both are derived from
an open entry with no live owner, because the process that died had no chance
to record either. The mixed arm is the acceptance test — it dies holding an
item its dependency blocked and an item that was executing, and the two must
come back classified differently.

The ordering a fan-out declared rides the opening, so an item that never
started can name what it was held behind: the mixed arm's restart reads
`fleet-3 <- fleet-2`, unchanged across the boundary. A dependency is not
scheduler state and is deliberately not recorded as one — an unmet dependency
is why an item is not yet ready, a slot is what a ready item waits for, and
`agentgraph` keeps `pending` and `queued` apart for that reason.

A declared dependency explains what an item was ordered behind, not whether that
dependency was ever met. What separates an item its upstream never released from
one that became ready and then waited for a slot is the queue record: the first
has none, the second names the ceiling that refused it.

## The scheduler-wait arms

Four arms ask a narrower question than the fan-out ones. Classification is
already right — `STARTED` settled that — so these ask whether the host can still
say **why** a ready item never started.

They are separate arms because `canStartLocked` decides in a fixed order:
slots, then writers, then claim. An arm that let an earlier check fire would
report that refusal as the later one, so each clears every check above the one
it measures — the slots arm refuses a reader, which is not a writer at all; the
writers arm leaves total capacity free and gives its two writers disjoint
paths; the claim arm leaves both ceilings free. The ceilings come from a project
`reasonix.toml`, which is where a person sets them, rather than from a poked
field.

`wait-claim` needs two fan-outs. A single fleet's preflight refuses concurrent
writers whose claims overlap, so the holder is dispatched as a background fleet
and the refused writer comes from a later one. Both it and `wait-transition`
wait on a signal the holder's own child sends once its slot is granted:
dispatching earlier lets the refused item win the race and start.

`wait-transition` answers what the cause means. It frees the ceiling the
refusal named while the item stays queued, held back by a different one, and
the graph still reports the first: one report, not two. So `WaitCause` is the
cause that **queued** the request, not the one holding it now — which is why
the durable record is named for entering the queue rather than for waiting.

Measured, all four: the refusal is reached in isolation, reported exactly once,
and read back after the restart with its cause intact. The three values are
kept apart rather than folded into "a ceiling refused it": the scheduler knew
which one, and a journal that discards a distinction the host actually drew
cannot get it back. A reader that only wants capacity-versus-claim can fold
them; the record does not fold them first.

A queue record proves more than the refusal. An item reaches the scheduler only
once its dependencies are answered, so a queued entry is durable evidence that
the dependency gate was crossed — which a declared ordering alone can never
say. That is the difference between the mixed arm's `fleet-3`, whose journal
reads `open` and nothing else, and a refused item, whose journal reads
`open queued(slots)`.
