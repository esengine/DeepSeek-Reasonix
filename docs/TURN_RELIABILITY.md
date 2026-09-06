# Turn reliability and interruption recovery

Tracks #9825, #9805, #9683 and the replay half of #9566: work that already
happened must never be repeated, results that never reached the model must
never be invented, and a turn must always end in a durable terminal state.

The design extends what exists: the `turns.jsonl` lifecycle ledger, the
session events, `InterruptedTurnRecovery`, and the `executeBatch` executor.
No parallel persistence system is introduced.

## Tool run states

Every tool call ends in exactly one `ToolRunState` in the ledger and in the
stored tool message. The two are produced by one classifier so they cannot
disagree.

| Situation | State | Automatic retry |
|---|---|---|
| Cancelled or refused before the start barrier | `cancelled` / `not_started` | allowed |
| Tool returned success | `completed` | never |
| Tool returned an explicit error | `failed` | read-only or idempotent tools only |
| Started, result not confirmed (cancel, crash, unconfirmed write) | `unknown` | never |
| Unknown, but a postcondition check proves the effect | `completed` + satisfied write | never |

The `ToolStarted` event is the barrier. A call that crossed it and produced
no confirmed result is `unknown`, even when the batch was cancelled later.
`not_started` is kept for old records; new readers treat it like `cancelled`.

## Execution rules

- Read-only calls fan out in parallel; writers, `bash`, and proxies run one at
  a time in provider order.
- After a writer fails, is refused, or ends `unknown`, later writers in the
  batch are skipped with a dependency notice. Read-only diagnosis still runs.
- Cancellation classifies each remaining call by the start barrier: started
  calls become `unknown`, the rest `cancelled`.
- Arguments of interrupted calls stay in the local `ToolCallRecord`; they
  never enter the model-facing recovery block.

## Recovery handoff

The next real user turn carries a bounded `<interrupted-turn-recovery>` block
listing `completed_tools`, `failed_tools`, `cancelled_tools`,
`not_started_tools`, `outcome_unknown_tools`, write postconditions, and
whether partial assistant output was excluded. It contains names, IDs, and
effect summaries only.

An identical re-issue of an `unknown` side-effecting call is refused until
its effects are inspected; satisfied write postconditions short-circuit to
"already done". Failed calls keep their paired error result and are not
listed as unknown.

The controller stamps the ledger turn ID on the handoff and merges an
earlier handoff when a turn is interrupted twice.

## Terminal turn status

Every turn ends in `completed`, `failed`, `interrupted`, or
`recovery_required`. A cancellation escalates to `recovery_required` when any
side effect is unproven or when the turn died before producing anything
(`silentInterruption`). The Desktop reads this from the `TurnDone` event.

## Provider transcript gate

Before a request is frozen, the same normalized view the adapters send is
validated: undecodable arguments, missing or misordered results, and orphan
results are refused before they reach the wire. Normalization already
repairs truncated arguments and dangling calls; the gate turns a normalizer
regression into a local error instead of a provider 400.

## Status

Landed (Phase 1, state correctness):

- explicit run states with local argument receipts and the start barrier
- one classifier for ledger events and stored messages
- `unknown` engages the batch dependency barrier
- `failed_tools` in the recovery handoff; turn ID stamping
- `recovery_required` terminal escalation
- transcript gate before every provider request

Next:

- Phase 2: ledger schema v3 with tool call records, checkpoint before the
  first side-effecting call, restart recovery of pending and unknown calls
- Phase 3: Desktop recovery card with inspect, mark-completed, and re-run
  (new attempt ID, original call ID never reused); silent-interruption notice;
  no automatic session fork for ordinary tool interruptions
- Phase 4: postcondition checkers for `git commit` and `bash`, checkpoint GC,
  recovery metrics
