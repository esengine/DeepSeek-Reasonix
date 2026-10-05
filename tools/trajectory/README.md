# progress_corpus

Extracts execution-progress metrics from a Reasonix trajectory export.

```bash
python3 tools/trajectory/progress_corpus.py session.json
python3 tools/trajectory/progress_corpus.py session.json --json --require-semantic
python3 tools/trajectory/progress_corpus.py session.json --transitions out.jsonl
```

Standard library only. Tests: `python3 -m unittest discover -s tools/trajectory -t tools/trajectory`.

## Three statements this tool is built on

**Total task tokens measure resource consumption; progress gaps measure
liveness. They are not interchangeable.** A task that spends 120M tokens
advancing steadily and one that spends 120M without advancing after the first
hour have the same total and nothing else in common.

**Proxy samples are suitable for historical incident analysis but must not be
mixed with semantic samples when calibrating stopping policy.** The two count
different things, and a quantile over both is a quantile over neither.

**Reported thresholds are descriptive, not prescriptive.** Nothing here
recommends a budget. A number that fits one incident is a property of that
incident until a corpus says otherwise.

## Schema v1 is frozen

Samples are only comparable while the definitions behind them hold still, and
nothing about a JSONL file says which definitions produced it. `schema_version`
does, and version 1 means exactly these:

| definition | where it lives |
| --- | --- |
| `ClassifyTodoTransition` — the six verdicts and how they are read | `internal/runtime/evidence/todo_progress.go` @ 2d379d411 |
| `stepIdentity` — stable id, else normalized text, never position | `internal/runtime/evidence/todo_progress.go` @ 2d379d411 |
| what advances a progress revision | `internal/runtime/agent/todo_progress_shadow.go` @ 2d379d411 |
| gap arithmetic — measured from the previous advance | `progress_corpus.py` @ 92d7e37de |
| episode segmentation — the three resolving kinds, censored tail | `progress_corpus.py` @ 92d7e37de |

Changing any of them changes what a number means. When one has to change, add
`schema_version: 2` and leave version 1 meaning what it meant — an in-place
edit makes two months of samples silently incomparable, and nothing in the data
would show it.

The first corpus is an audit of these definitions, not of the agent. What it
has to settle first is whether the text-identity fallback reports renames as
replans; that is the cheapest possible moment to fix, because no execution
decision reads a progress revision yet.

## Two sources of progress

`progress_source` and `progress_fidelity` are part of every sample, not a note
in the output.

| source | fidelity | what advances the counter |
| --- | --- | --- |
| `todo_progress` | `semantic` | the canonical task list moved: a step completed and execution went on |
| `complete_step` | `proxy` | a sign-off call succeeded |

The proxy is what an export predating the `todo_progress` frame can offer. It
**overcounts**: a sign-off landing on a step already complete is a renewal, and
renewals advance nothing.

That direction is what makes one proxy number still usable. Removing false
advances can only merge adjacent gaps, never split them, so

```
proxy max_tokens_between_progress   is a lower bound on the semantic one
proxy max_wall_between_progress     is a lower bound on the semantic one
```

Everything else changes shape when the false events go: `p50`, `p90`,
`max/p50`, the event count, and the transition mix all need the canonical
session to recompute. The table above is why `--require-semantic` exists —
a corpus meant to calibrate policy must refuse a silent downgrade.

## The incident this was built from

A 15.6-hour session, read in proxy mode:

```
total_tokens                393.95M
progress_events             25          (proxy — an upper bound)
p50_tokens_between_progress 0.22M
max_tokens_between_progress 348.81M     (88.5% of the total, 12.32h)
max_to_p50_ratio            1581x
content_to_progress_ratio   43.6
```

The two bounds that survive the proxy caveat: at least 348.81M tokens and at
least 739 minutes passed between two host-observed advances. That is the claim
liveness work rests on, and it is stated in resources rather than rounds.

## Replan episodes

`--replan-episodes out.jsonl` segments the transition stream at every replan and
records what resolved it, one object per episode:

```json
{"replan_at": 812.4, "replan_tokens": 12400000, "outcome": "advance",
 "outcome_at": 851.2, "outcome_tokens": 13100000,
 "tokens_to_outcome": 700000, "wall_to_outcome": 38.8}
```

`outcome` is one of four, read from the resolving transition's own kind:

| outcome | the replan was followed by |
| --- | --- |
| `advance` | a step completing and execution moving on |
| `terminal` | the last step completing |
| `replan` | another change of plan |
| `end` | nothing — the sample ended first |

Three properties are deliberate. `terminal` is not folded into `advance`, even
though a progress revision counts both: "the plan finished" and "the plan moved
one step" are different answers to what a strategy change bought. A `rewrite`
does not end an episode, so restating the steps after changing them stays
inside the interval rather than resolving it. And an episode still open when
the sample ends is written as `end` rather than dropped, because a replan that
nothing ever followed is the case a survivor-only view would lose.

Semantic samples only: the proxy source carries no replan verdict, so there is
nothing to segment on and the tool refuses rather than inferring one.

This is a segmentation, not a statistic. Nothing here aggregates episodes or
names a healthy rate — what fraction of replans lead anywhere is a question for
a corpus, and the first thing that corpus has to settle is whether the text
identity fallback (below) is reporting renames as replans.

## Known fragility

Semantic transitions are read back from the rendered trajectory line
(`todo advance · content 4 · plan 1 · progress 2`), because that is what an
export carries. Step identity falls back to normalized text when a list carries
no `step_id`, so a step renamed mid-run reads as one identity leaving and
another arriving — a replan by this tool's definition, and possibly a rewrite by
a person's. The `replan / all transitions` ratio is the diagnostic; a corpus
full of `replan → replan` at small token gaps is a reason to check identity
before concluding anything about strategy changes.

A renderer change breaks the parse — deliberately loudly: a
line that starts with `todo ` and does not parse raises rather than reading as
"no semantic progress", which would quietly turn every later sample into a
proxy. If the export ever carries the frame's fields structurally, read those
instead.

# lease_cost

Reports what the workspace write lease cost, read from session wire logs.

```bash
python3 tools/trajectory/lease_cost.py ~/.reasonix          # every project
python3 tools/trajectory/lease_cost.py ~/.reasonix/projects/<slug>/sessions
python3 tools/trajectory/lease_cost.py session.wire.jsonl --json
```

Sessions are kept per project, so the root to hand it is usually
`~/.reasonix` (or `$REASONIX_STATE_HOME`), not `~/.reasonix/sessions` — that
last one holds only the sessions opened outside a project.

Standard library only. Tests: `python3 -m unittest discover -s tools/trajectory
-t tools/trajectory`.

## What this measures, and what it does not

**The population is holds, not sessions, and not waits.** One account is closed
per release of the lease. A read-only turn never takes the lease and is not in
the denominator; a session that wrote and never waited is, and is most of it.
A rate quoted over contended holds alone is not a rate of anything.

**The wait count cannot be taken from the notices.** A wait that clears inside
the one-second grace never becomes one, and those are the common case. The
kernel counts every contended acquisition and separately counts the subset that
was reported; `contended` and `reported` are not the same number and neither
substitutes for the other.

**`idle/held` is the question, not `waited`.** Waiting says the serialisation
was felt. The idle share says whether it needed to be: it is the part of each
hold that came after the last write asked for the lease, which is what a
release at the last verification rather than at the end of the turn would give
back. A high contention rate with a low idle share means the lease is being
used exactly as long as it is needed.

**An incomplete log makes every number a floor.** A lease account is written
when a turn ends, so the sessions that lose frames to the size cap are the long
ones — which are also the ones most likely to have held the lease a while. The
count of incomplete logs is printed beside the rest for that reason, and an
unreadable truncation witness counts as incomplete rather than as intact.
