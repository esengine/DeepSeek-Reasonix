# workgroup assay

Two assays, one question each. Keeping them apart is the point: mixing their
turns would put two sampling mechanisms in one denominator.

```
go run ./tools/workgroupassay            # P2-2e-natural, the current one
go run ./tools/workgroupassay -root DIR
```

## P2-2e-natural — is ordinary use worth folding?

The live assay. Its protocol is in `protocol.go` so changing it is a reviewable
diff.

- **Sample**: the first 20 eligible authored turns produced *after* the freeze
  timestamp by ordinary Studio use, in the order they happened. Not the last
  20, not a chosen 20, not 24 because the answer landed near a line.
- **Eligible**: the turn carries a named `authoredTurn` — which only the
  current schema emits, and which a synthetic continuation never gets — and its
  trajectory reads `complete`, because a prefix cannot answer a question about
  a whole turn.
- **In**: quiet turns with no tool call, one-call turns, executor-only turns,
  plan-and-execute turns, and whatever approvals and asks happen on their own.
  A turn with nothing to fold is evidence about coverage, and dropping it would
  answer a different question and flatter the feature.
- **Out**: turns written for this experiment, benchmark and fixture runs, and
  anything shaped to call more tools than the work needed. Enforced, not
  merely stated: a workspace under a temp root is where a driven run lives, so
  its sessions never enter the sample. Before that check the only thing keeping
  a scripted run out was that it happened before the freeze, which says nothing
  about the next one.
- **No verdict before the sample is whole.** Under 20 the tool prints the count
  and stops.

## P2-2d-designed — how good can it be where it fits? (completed)

A written task list of 12 turns, run once against a real model. It is recorded
in `protocol.go` as a reference condition and is **excluded from the natural
verdict**. Raw trajectory, task list and result:
`~/.reasonix/research/p2-2d-designed/`.

It cost ¥2.13 and its most durable result was not a number: on its ninth turn
it broke an invariant ten fixtures had agreed held, because a `Partial=true`
dispatch is a transport statement and not a promise that a completion frame
follows. See `internal/state/workgroup`.

## The verdict

NO-GO requires **both**: more than half of groups are a single call, **and**
fewer than 40% of authored turns contain a group of three or more. Anything
else is not an automatic go — it hands the decision to a human reading the
distributions, the group shapes, and the coverage number below them.

Coverage — how many turns have no assistant-owned call at all — is reported and
deliberately **not** gated. Adding it to the gate now would be redrawing the
line after seeing a sample; leaving it out of the report would let a low
singleton rate read as broad reach when a fold might touch half the turns.
