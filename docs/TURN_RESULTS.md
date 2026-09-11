# Turn results

[简体中文](TURN_RESULTS.zh-CN.md)

After a turn changes files or records checks, Desktop shows **Turn result**
below the answer. It reports two independent facts: confirmed net file changes
and recorded check outcomes. A passing check is not an overall completion grade.

- **View changes** opens that turn's frozen diff. Repeated edits to a file count
  once, relative to its first captured state in that turn. Changes restored to
  their original content contribute zero lines. Existing dirty work is part of
  the baseline, not attributed to the agent.
- **View check details** shows recorded commands, exit codes, stale/interrupted
  outcomes, and expandable logs. **Checking…** only appears for a host-identified
  check that has actually entered execution.
- **All current workspace changes** returns to the existing workspace view.
  That view can include other turns and edits made outside Reasonix.

These controls inspect results. They do not run, retry, or schedule commands,
and require no new setting.

## Coverage and history

**Partial statistics** means the snapshot observer cannot reliably attribute all
changes to this turn, or content exceeded the bounded capture/diff budget.
Confirmed counts remain visible; unknown changes are not guessed from Git HEAD,
tool-call counts, or the number of mutation receipts.

Binary changes and mode-only changes have file entries without invented line
counts. Uncounted large changes and unavailable patch details are labelled.
Moves use the paths recorded by the existing capture mechanism; the result view
does not infer renames.

A historical card keeps its recorded statistics. Opening it never recalculates
the diff against today's disk contents. If checkpoint details were pruned, the
card retains its summary and explains that the detail is unavailable. Old
sessions without result metadata show unavailable statistics and incomplete
check information, rather than zero changes or an implied pass.

Logs are read from existing local session messages, using both the provider call
ID and the stable local result-message ID captured at turn completion. Reused
provider IDs cannot redirect an older card to a newer log. Ambiguous, absent,
or cleared sources are explicitly unavailable. Display is capped at 2 MiB, with
a truncation notice when only the tail fits.

## Implementation contract

The host adds optional fields to the existing `turn_done.receipt`:

- `diff`: checkpoint turn, immutable result ID, coverage, files, and exact
  added/removed line totals. Event/history summaries omit patches.
- Per-check `toolCallId`, `toolResultId`, `exitCode`, and `interrupted`.
  Existing command classification and completion policy remain authoritative.
- `tool_progress.tool.verifying`: a host-only execution signal, with no
  additional model-facing tool or prompt.

The checkpoint store freezes results while turn admission is closed, using its
existing nonblocking mutation barrier and validating post-write fingerprints.
Each turn has a 2 MiB content/patch processing budget. Approximate diff-engine
fallback counts are never reported as exact. Results share checkpoint retention;
patch metadata is included in the disk budget.

Desktop reuses its existing display sidecar and durable event replay. The
transcript's common turn projection places result cards after answers and keeps
their mounted IDs stable. Session/tab/result keys fence delayed diff and log
responses. Provider prompts, tool schemas, and execution permissions are unchanged.

## Verification

Regression tests cover net repeated edits, no-ops, pre-existing dirty files,
deletion, binary/mode changes, active and external writers, size limits, reopen,
old readers, cancellation/error terminal publication, check exit codes, stable
log identity, display-sidecar replay, duplicate result updates, concurrent
checks, and delayed responses after session replacement.

For a browser check, run `pnpm dev` in `desktop/frontend`, then open
`/bench/turn-result.html` at the URL printed by Vite. This fixture uses the real
Transcript, reducer, result panel, and diff renderer with mocked execution data.
It provides success, failure, no-check, stale, interrupted, running, partial,
legacy, and historical scenes, plus theme/width and cleared-data controls.
Use `?transcriptRenderMode=windowed` to exercise the shared window adapter.

Browser screenshots from this fixture are UI evidence; controller/checkpoint
tests separately prove real storage and ownership behavior.
