# Local Sub-Agent Progress

Status: **implemented** — per-child progress previews for local sub-agent runs
(`task`, `read_only_task`, `parallel_tasks`, `fleet`) in the desktop app and the
CLI, on top of the persisted transcripts and `read_subagent_result` (see
[`CHECKPOINTS.md`](CHECKPOINTS.md) for the persistence model).

## Goal

While a sub-agent works, the user should see **what it is doing** without the
sub-agent's reasoning/text bodies entering the parent conversation: a progress
card shows the child's phase, running elapsed time, and recent activity; the
desktop card can be expanded for a bounded reasoning / response / notice
preview, and the CLI shows the same previews in `/verbose` mode. Everything is
zero-configuration — there are no new settings.

## Wire contract

Progress previews reuse the existing `ToolProgress` event with four reserved
`Tool.Name` values. These names are an internal contract between the agent
progress tracker and local frontends; they must never be presented as
provider-visible tool names:

| Name | Payload |
|---|---|
| `reasonix.subagent.status` | exactly one of `queued`, `running`, `reasoning`, `responding`, `tool`, `retrying`, `completed`, `failed`, `cancelled` |
| `reasonix.subagent.reasoning` | bounded UTF-8 text delta (the child's thinking) |
| `reasonix.subagent.text` | bounded UTF-8 text delta (the child's response preview) |
| `reasonix.subagent.notice` | bounded UTF-8 text delta (the child's notices) |

Field conventions:

- `Tool.ID` — the child task card ID (progress lookup is by ID, never by body).
- `Tool.Output` — the phase value (status) or a text delta (previews).
- `Tool.Truncated` — set when this round's preview was truncated or merged.
- `Tool.DurationMs` — the final duration, carried on terminal status events.
- `Tool.ParentID` — follows the existing nesting relationship (empty for a
  top-level `task`; the group call ID for `parallel_tasks`/`fleet` children).

## Behavior

State machine (emitted by the unified run chain in `RunProfileSpec`, shared by
`task`, `read_only_task`, `parallel_tasks`, and `fleet` — no per-entry copies):

- Foreground runs start with `running`.
- Background runs emit `queued` at registration and `running` once the job
  acquires its execution slot.
- `parallel_tasks`/`fleet` group cards get an explicit lifecycle of their own:
  `running` when children start and exactly one terminal after every child
  settles (`completed`, `cancelled` for cancellation/deadline, `failed` when
  any child failed or the call errored — including validation failures).
  Frontends never infer group completion from the children observed so far,
  since background children dispatch asynchronously and a fast first child
  can finish before later ones appear.
- The child's `Reasoning` / `Text` / `Notice` / `Retrying` events become the
  corresponding preview channels; the child's real tool activity flips the
  phase to `tool` while the nested tool cards render as before.
- Every run emits exactly **one** terminal status: `completed` on success,
  `cancelled` for context cancellation or deadline, `failed` for provider,
  tool, storage, or panic errors. Pending previews are flushed synchronously
  before the terminal; events arriving after the terminal are ignored.

Pacing and memory bounds (per parent task group):

- One pending slot per (child, channel); previews merge for up to 250 ms before
  one event is emitted, so deltas never accumulate unboundedly.
- At most 32 non-terminal events/sec per group — phase transitions and content
  previews share the same budget, round-robined across children so one hot
  child cannot starve the others. Only the initial `queued`/`running` states
  and the terminal event bypass the limit.
- When the budget trims buffered content, the loss is flagged `Truncated` on
  the next actually-emitted channel (or surfaced as a truncated notice at the
  terminal flush), so frontends always learn that some preview was dropped.
- Each child's unsent pending buffer is capped at 8 KiB total (notice is
  dropped first, then reasoning, then text); overflow keeps a UTF-8-safe tail
  and sets `Truncated`. The desktop retains per-channel preview caps (8 KiB
  reasoning/text, 2 KiB notice); the CLI keeps 4 KiB reasoning/text tails for
  `/verbose`.

What is **not** done:

- The child's `Message`, reasoning, and text bodies never enter the parent
  transcript or provider context.
- No new event kinds, no new wire fields, no provider tool list/schema/system
  prompt changes, no configuration.
- Previews are never persisted: after a restart the complete sub-agent
  transcript (and `read_subagent_result`) remains the source of truth.
- ACP and bot consumers keep ignoring `ToolProgress` bodies entirely.

## Desktop

- A sub-agent tool card shows a phase chip (phase + running elapsed + "N s
  ago" recent activity) in its header; the chip ticks once a second while the
  child is live and settles to a phase + duration summary.
- Expanding the card shows isolated reasoning / response preview / notices —
  never mixed with ordinary tool output.
- A background call that already returned its job id stays in the running
  state while child progress is non-terminal; `parallel_tasks`/`fleet` group
  cards settle only from their own lifecycle terminal event, so neither a
  job-id result arriving before any child nor a fast first child finishing
  before later children dispatch can settle the group prematurely.
- `completed` / `failed` / `cancelled` reuse the existing done / error /
  stopped visuals; after a terminal the card folds by default unless the user
  explicitly expanded it.

## CLI

- Each child keeps its own progress state and a fixed transcript slot keyed by
  its call ID — independent of the single live tool stream, so concurrent
  children never cross-stream.
- By default only the phase, elapsed, and recent activity are shown; the
  reasoning/text bodies appear in `/verbose` (Ctrl+O) mode, bounded to the
  recent 4 KiB tails.
- Terminal children fold to a one-line summary; verbose keeps the bounded
  preview.
- Terminals without in-place redraw (Termux native scrollback) print a status
  line on phase changes and terminal only; verbose previews print at most once
  every 2 seconds per child.

## Contract stability

Frontends match the reserved names by the `reasonix.subagent.` prefix, so a
future channel added by a newer agent is ignored (never appended to ordinary
tool output) by older frontends.
