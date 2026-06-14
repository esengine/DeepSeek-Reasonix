# Design: Checkpoints & Rewind

Status: **Phase 1 + 2 implemented** — snapshot store, capture seam, the Esc-Esc /
`/rewind` CLI picker, and the desktop hover-rewind, with the full Claude Code menu:
restore code / conversation / both, fork-from-here, and summarize from / up to
here. Snapshot-based and aligned with Claude Code. An optional git-backed mode is
the remaining (lower-priority) follow-up. Tracks the most requested missing
capability from v1 — an edit safety net / undo.

## Goal

Let a user rewind a session to a previous point and restore **code**,
**conversation**, or **both** — without touching their git history. Aligned with
Claude Code's rewind (Esc-Esc / `/rewind`), driven identically from the CLI and
the desktop.

## Mechanism: file snapshots, not git

Like Claude Code (and v1's `checkpoints.ts`), checkpoints are **file snapshots**,
independent of git:

- **Zero git pollution** — never commits, stages, or touches `.git/`. Works in a
  non-git directory.
- **Tracks only previewable edit-tool changes** — `write_file` / `edit_file` / `multi_edit`.
  File moves via `move_file` follow the same workspace permission boundary, but
  are not yet represented in checkpoint previews.
  `bash` side effects are **not** tracked (no way to know what a shell command
  touched), exactly as Claude Code. Risky bash is already permission-gated.
- Full pre-edit content snapshots (simple; storage bounded by retention, below).

An optional **git-backed mode** (v1's `auto-git-rollback`) is a possible Phase 2
for users who want git-level safety; it is explicitly out of scope here.

## Anchors & capture

- **One checkpoint per user turn.** A checkpoint opens when a turn starts
  (`Controller.Send` / `runTurn`), labelled with the user prompt.
- **Pre-edit snapshot.** In `agent.(*Agent).executeOne`, before running a tool
  whose `ReadOnly()` is false and which implements `tool.Previewer`, call
  `Preview(args)` → `diff.Change{Path, Kind, OldText}` and record a snapshot of
  that file into the active checkpoint. `tool.Previewer` already exists and the
  file-writers implement it, so this is one centralized seam — no per-tool code.
  - Dedup per path per turn: only the **first** touch is snapshotted (that is the
    file's turn-start content).
  - `Kind == create` (file did not exist) → store `Content = nil` so a restore
    *deletes* it. `modify`/`delete` → store `OldText`.
  - `bash` has no `Previewer`, so it is naturally excluded — matching the
    "edit-tools only" contract.

## Data model

```go
type FileSnap struct {
    Path    string  // workspace-relative
    Content *string // nil → file did not exist at the anchor (restore deletes it)
}

type Checkpoint struct {
    Turn   int        // user-message index this anchors (0-based)
    Time   time.Time
    Prompt string     // user message text — the picker label
    Files  []FileSnap // distinct files touched during this turn, turn-start state
}
```

## Storage

- **Sidecar to the session**, under `config.SessionDir()`: `<session-id>.ckpt/`
  with one JSON per checkpoint plus a small index (v1's layout — cheap delete, a
  corrupt snapshot only loses itself). Kept separate from the message JSONL
  (`agent.Session.Save`) so the session format is unchanged.
- **Persists across sessions** — resuming a session re-loads its checkpoints, so
  rewind works after a restart (Claude Code parity).
- **Retention**: prune with the session (default ~30 days, configurable), to bound
  disk from full-content snapshots.

## Controller API (the one seam both frontends drive)

Checkpoints live on `control.Controller`, beside `SetPlanMode` / `Compact` /
`NewSession`, so the terminal TUI, the desktop webview, and the HTTP/SSE server
drive rewind identically and none re-implement it.

```go
type RewindScope int // Code | Conversation | Both

func (c *Controller) Checkpoints() []CheckpointMeta      // for the picker
func (c *Controller) Rewind(turn int, scope RewindScope) error
```

- **Code**: for every checkpoint from `turn` to the latest, take the earliest
  `FileSnap` per path and restore each file to that content (delete if `nil`) —
  i.e. undo all edits made at or after `turn`. Path-escape re-checked against the
  live workspace root.
- **Conversation**: truncate `Session.Messages` to just before turn `turn`'s user
  message, re-`Save`, and emit the truncated history as events so the frontend
  re-renders. The turn's prompt is restored into the composer for re-send/edit
  (Claude Code behavior).
- **Both**: code + conversation.

A `Rewound` event (or reuse of a history-replace event) lets every frontend
re-render uniformly.

## CLI UX (aligned with Claude Code)

- **`Esc Esc`** with an empty composer, or **`/rewind`**, opens a picker listing
  each user turn (time + which files it changed). `chat_tui` already tracks the
  double-Esc timing.
- Select a turn → sub-menu: **`[code+conversation] [conversation] [code] [cancel]`**.
- On a conversation/both restore, the selected prompt is prefilled into the
  composer.

## Desktop UX (aligned with the VS Code extension)

- Each user message in the transcript gets a hover **rewind** control → menu:
  **rewind code / rewind conversation / both / fork-from-here**.
- It calls the same `controller.Rewind` over the Wails binding; the controller's
  event stream pushes the restored state and React re-renders. No rewind logic in
  the frontend.

## Non-goals & edge cases

- **bash / external side effects** (`rm`, `mv`, DB writes, deploys) are not
  tracked — rewind cannot undo them (Claude Code parity).
- **External edits between turns**: a snapshot holds the file's turn-start
  content, so restoring overwrites edits made outside reasonix in the meantime.
- **Deletions**: an edit-tool deletion is restorable (snapshot has the content); a
  `bash rm` is not.
- **Large files**: full snapshots — retention cleanup bounds disk; revisit dedup
  (content-addressed snapshots) if it becomes a problem.

## Phasing

1. **Phase 1**: snapshot store + `executeOne` capture seam + `Controller.Rewind`
   (code/conversation/both) + CLI picker (Esc-Esc + `/rewind`).
2. **Phase 2**: desktop hover-rewind UI; "fork from here"; "summarize from/up to
   here"; optional git-backed mode.

## Open questions

- Snapshot on `/compact` and on `NewSession` boundaries?
- Default retention window and whether to expose it in `[checkpoints]` config.
- Content-addressed dedup vs one-file-per-snapshot from the start.
