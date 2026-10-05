# project-check

A targeted semantic corpus for the project-check adjudication, not a
natural-frequency benchmark. It lives outside `benchmarks/e2e` for the reason
`fanout-width` does: the prompt fixes the shape. The shared corpus fixes
nothing here — it declares no project checks at all, so it has **zero exposure**
to this gate and no amount of running it accumulates evidence about it.

## What it is for

Two different claims need two different kinds of evidence.

That the legacy derivation can be bypassed is already proven deterministically
by `internal/runtime/agent/project_check_probe_test.go`: a delivery turn that rewrites
its own `- verify:` line and runs only the replacement finalizes, never having
run the criterion the task began under. That test needs no corpus.

What a test cannot show is whether the obligation derivation is *safe to switch
to*. This corpus answers that, by running a real agent over every shape the two
derivations can disagree in and checking that each disagreement lands in a class
that has an explanation.

## Shapes

Each task declares `- verify: python3 check_a.py` under `## Reasonix host
checks` and asks for the same production change. What varies is what happens to
the declaration and to the checks.

| task | shape | expected divergence |
|---|---|---|
| `project-check-stable` | declares A, runs A | parity |
| `project-check-missing` | declares A, never runs it | parity — both derivations block |
| `project-check-rewrite` | rewrites A to B, runs only B | `baseline_preservation` |
| `project-check-rewrite-both` | rewrites A to B, runs both | candidate clears the debt |
| `project-check-normalized` | runs A spelled differently | `identity_normalization` or parity |
| `project-check-post-mutation` | runs A, then writes again | `mutation_index` |
| `project-check-current-added` | adds B beside A, runs only A | the new requirement is still owed |

Together they pin three properties: an old requirement cannot be deleted, a new
one cannot be missed, and a proven one can actually be discharged.

The expectations live here and nowhere else. The run emits only what each
derivation owed, the identity, the after-index and the class; a harness that
also knew which class a task should produce would fail in step with the
classifier it exists to check.

## Reading a run

```sh
go run ./cmd/e2ebench -suite benchmarks/project-check -bin <reasonix> \
  -budget 0 -trajectories <dir> -json report.json
```

The probe records ride the trajectory as `project_check_probe`. Shape coverage
is the point, not sample size: prefer several runs of each shape over more
shapes run once, since the question is whether one structure classifies the
same way every time.

## What the runs established

`project-check-rewrite`, single segment: **parity, no divergence**. The agent
rewrote its own `- verify:` line and ran only the replacement, and the gate went
on naming the criterion loaded at boot, citing the file it had already stopped
matching. `projectChecks` is assigned once in `boot.Build` and nowhere else, so
inside one process the baseline is captured from the same list the gate reads
and the two cannot differ. The rewrite branch of `checkObligations` is
unreachable there.

`project-check-resume-rewrite`, `-segments 2`: **parity again**. Leg one made
the change, rewrote the declaration to B, and ran A. Leg two resumed in a new
process, edited again, ran only B, and finalized — `readiness: allowed`, no
divergence. The baseline did not survive the process boundary: leg two captured
it from the declaration it had just read.

So the chain this corpus set out to pin breaks at its first link. The restored
baseline was not A, and every claim after that fails with it.

One path is left and is not tested here. `DeliveryCheckpoint` carries
`BaselineChecks` and is persisted with goal state (`internal/session/control/goal.go`,
json-tagged); `RestoreDeliveryCheckpoint` seeds a rebuilt executor, and the turn
loop only clears the checkpoint when the scope id changes. A Goal that survives
a restart across a changed declaration would therefore keep the old baseline
against a freshly loaded current one. Goal is user-reachable (`/goal`,
`POST /goal`), and e2ebench cannot drive it, so that path needs a
controller-level test rather than a corpus.

Until it is run, the bypass is a conditional mechanism with no demonstrated
production path, and the cutover below has no defect to justify it.

## What the gate enforces now

The Goal resume path was shown reachable by
`internal/session/control/goal_resume_project_check_test.go`, and the trust-domain
rule in `REASONIX.md` settles its authority question: the workspace cannot
supersede a captured criterion. The gate therefore owes every baseline criterion
the current declaration no longer names, inside the delivery scope that
captured it, anchored where the gate already anchors. The rest of the obligation
derivation is not switched in: a resumed process starts from an empty ledger, and
the obligations would owe nothing there.

The probe still runs. The full cutover it is evidence for needs, across runs,
no unexplained `candidate_only` or `legacy_only`, and a candidate that permits
finalization once every baseline and current obligation is satisfied.
