---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-11-09
---

# turnprofile

Splits each turn of a `--trajectory` JSONL into exclusive time buckets, so
"where does a turn spend its wall-clock" is answered from a recorded run.
Read-only: it only reads the files it is given.

```bash
reasonix run --trajectory run.jsonl -p "..."
python3 tools/turnprofile/turnprofile.py run.jsonl [more.jsonl] [--per-turn] [--json]
```

Standard library only. Tests:
`python3 -m unittest discover -s tools/turnprofile -t tools/turnprofile`.

## What a number means

- **Anchor.** A turn runs from its `turn_started` record to its `turn_done`
  event, or to the `turn_completion` record headless `run` writes instead. A turn
  with neither is censored and left out of every aggregate.
- **Population.** Every turn in the files given. The tool does not sample,
  weigh or trim, so a population of three turns says nothing about a fleet.
- **Exclusive.** Every millisecond of a turn goes to exactly one bucket, so the
  buckets sum to the turn's wall time. Where intervals overlap, the earlier
  bucket in this order wins.

| bucket | counts | taken from |
| --- | --- | --- |
| `user_wait` | approval or ask pending, to the tool's start (or the turn's end when it never started) | `approval_request`, `ask_request`, `tool_result.startedAt` |
| `compaction` | a compaction pass | `compaction_started` to `compaction_done` |
| `tool` | a tool executing, parallel calls counted once; a subagent's whole run is inside its tool call | `tool_result.startedAt` to `endedAt` |
| `model_discarded` | a sampling attempt that was thrown away | `stream_attempt` begin to discard |
| `model_ttft` | request sent to first output | begin plus `firstTokenMs` |
| `model_gen` | first output to the end of the stream | that point to commit |
| `host_setup` | before the first request | turn start to first begin |
| `host_between_rounds` | no model, no tool, between requests | the gap |
| `host_tail` | after the last request or tool | to the turn's end |

`tool busy (sum)` beside the union is the work the tools did in total; a
difference between the two is parallelism.

## What it does not say

- `host_*` is what nothing else claimed. It is a measure of host overhead only
  to the extent the events bracket the work: a host stall inside a recorded
  interval is counted as that interval.
- `user_wait` ends at the tool's start, an upper bound on the wait.
- Timestamps are the recorder's observation times, so a stream's first output is
  placed by the provider-reported `firstTokenMs`, not by a second clock.
- It says where time went, never what would be saved by moving it.
