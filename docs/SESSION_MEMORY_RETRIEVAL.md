# Session History and Synthesis Memory Retrieval

This document describes the lightweight retrieval layer added for session
history and saved memories. It is the implementation note behind the `history`
and `memory` tools, the archive-on-forget behavior, and the fresh human approval
gate for agent-written memory.

## Goals

- Bring useful past-session context back without injecting dynamic history into
  the stable system prompt.
- Keep Reasonix cache-first: stable prompt bytes stay stable across turns, while
  history and saved facts are fetched on demand.
- Avoid a heavy retrieval dependency. The implementation is pure Go and does not
  introduce SQLite, CGO, a vector database, or an embedding model.
- Make memory trustworthy. Agent-initiated memory writes and archives must be
  visible to the user and approved every time.
- Preserve traceability. A wrong memory should stop affecting the agent, but the
  removed document should remain inspectable.

## Non-Goals

- This is not semantic embedding search. It is lexical retrieval with BM25,
  tuned for code, commands, error phrases, filenames, and explicit decisions.
- This does not auto-summarize every session into memory. The `memory` layer is a
  synthesis cache: only stable conclusions that the user approves become saved
  documents.
- Archived memories are not active knowledge. They exist for audit and recovery,
  not for recall.

## Retrieval Core

`internal/retrieval` contains the shared retrieval primitives:

- tokenization for Latin words and CJK runes;
- document-frequency and BM25 scoring;
- compact snippets around query terms;
- `KeepTopRelativeScore`, which keeps the best hit but trims weak trailing hits
  below a relative score floor.

The relative score floor is intentionally small (`0.15`) and applied after
sorting. It prevents common-word-only matches from crowding out the useful hit,
while still preserving multiple close matches when they are genuinely relevant.

## Session History Tool

The `history` tool is read-only and lives in `internal/history`.

It supports two operations:

- `search`: rank saved session records by BM25.
- `around`: read a bounded transcript window around a returned hit.

Search input can be scoped:

- `project`: current session directory only.
- `global`: current session directory, user-global session directory, and
  compacted-history archive directory.

Search indexes these record kinds by default:

- user text;
- assistant text;
- tool inputs;
- tool errors.

Normal tool output is excluded by default because it can be large and noisy. It
can be requested explicitly with `kind=["tool_output"]`, optionally filtered by
`tool_name`.

`around` enforces path confinement. It only accepts paths under the configured
session or archive roots, so a model cannot use the tool as a general file reader.

When search returns no hits, the tool explicitly tells the agent that zero
results are not proof that an event never happened. It suggests retrying with
rarer terms, widening scope, or including tool output only when needed.

## Saved Memory Recall Tool

The `memory` tool is read-only and lives in `internal/memory/recall.go`.

It supports:

- `search`: BM25 over active memory files.
- `read`: return one full active memory by name.
- `list`: show the active memory index, optionally filtered by type.

Only active memories from the project memory store participate. Archived memory
files are excluded from `search`, `read`, and `list`.

The searchable text combines:

- slug/name;
- title;
- normalized type;
- description;
- body.

This makes short user-facing descriptions useful, while still allowing recall by
the detailed body when the agent knows a rare phrase from the saved fact.

## Memory as Synthesis Cache

Reasonix treats saved memory as a synthesis cache rather than as a raw transcript
cache.

The intended workflow is:

1. The agent searches `history` or `memory` when it needs old context.
2. If retrieval produces a stable reusable conclusion, the agent proposes a
   `remember` write.
3. The user reviews and approves or denies the write.
4. Future sessions can reuse the saved document directly.

This avoids repeatedly paying retrieval cost for the same stable conclusion,
while keeping the saved set small and auditable.

## Desktop Candidate Suggestions

The desktop Memory page can scan recent local sessions and produce draft
candidates:

- memory candidates from explicit long-lived preferences, rules, or project
  conventions in recent user turns;
- skill candidates from repeated workflow categories across recent sessions.

This is intentionally a suggestion layer, not an automatic writer:

- scanning can be run manually from the Memory page. Users may also enable a
  desktop UI preference that scans automatically when the Suggestions tab opens;
- candidates show their proposed body plus short evidence snippets before any
  write;
- accepting a memory candidate writes through the controller's active memory
  path, so the current session gets the same transient turn-tail update as a
  `remember` write;
- accepting a skill candidate writes through the normal skill store, preserving
  skill name validation, scope handling, and no-overwrite behavior.

No candidate scan changes the stable system prompt or provider-visible tool
schema. Saved memories and created skills become part of the stable prefix only
through the existing next-session discovery path.

## Archive-on-Forget

`forget` no longer permanently deletes the memory file. It removes the memory
from the active index and moves the file into `.archive/` with a timestamped
filename:

```
.archive/<UTC timestamp>-<name>.md
```

The active store and recall tool ignore archived files. Local management
surfaces still expose them for traceability:

- `/memory`;
- CLI/TUI memory views;
- desktop memory panel.

This is important because an incorrect memory can be more disruptive than no
memory, but a hard delete makes it difficult to audit how the agent reached a
bad conclusion.

## Human Approval Contract

Agent-initiated `remember` and `forget` calls require a fresh approval every
time.

The controller treats these tools like plan approval:

- Auto approval and YOLO/full-access mode do not bypass them.
- Guardian/safety review cannot allow them on the user's behalf.
- Session grants and persistent allow rules are not created for them.
- Pending memory approvals are not drained when the user toggles auto approval.
- Non-interactive headless runs and sub-agents refuse them instead of treating
  `Ask` as autonomous allow.

The approval subject is generated from the tool arguments before the
`ApprovalRequest` event is emitted:

- `remember` shows a compact preview of the name/title, normalized type,
  description, and body.
- `forget` shows the memory name being archived.

External notification hooks only receive the tool name, not the memory body,
because notification channels may be less private than the local UI.

User-initiated memory edits in the desktop panel or CLI remain direct user
actions and do not go through the agent approval prompt.

## Boot Wiring

`internal/boot` registers the tools in the shared registry:

- `history`;
- `memory`;
- `remember`;
- `forget`.

The saved memory index still folds into the system prompt once at session start,
after the base prompt. This preserves the cache-first prefix contract. Mid-session
memory changes are injected only as transient turn-tail notes and become part of
the stable prefix on the next session.

## UI and CLI Surfaces

Local management surfaces distinguish active and archived memory:

- Active memories can be searched, read, and used by the agent.
- Archived memories are read-only audit entries.
- Candidate suggestions are drafts until the user confirms them.

The desktop `Memory()` payload always returns non-nil arrays for docs, facts,
archives, and scopes. This is a Wails JSON contract: nil Go slices encode as
`null`, while the frontend expects arrays for `.map` and `.length`.

## Test Coverage

The change is covered across layers:

- retrieval scoring, snippets, tokenizer behavior, and relative-score trimming;
- `history` search, global/archive scope, tool input/error indexing,
  common-word-noise trimming, path confinement, and `around`;
- `memory` search/read/list, type filtering, 0-result fallback guidance, archived
  memory exclusion, and validation;
- archive-on-forget file movement, index updates, timestamp parsing, ordering,
  and read-only file repair;
- controller approval behavior under ask/auto/YOLO, including fresh approval and
  approval-preview visibility;
- boot-level tool registration and real model tool-call execution;
- desktop `Memory()` payload shape for active and archived facts;
- desktop memory/skill candidate generation, confirmation writes, and non-nil
  suggestion arrays;
- frontend CSS and TypeScript checks with generated Wails bindings.

## Operational Notes

- Prefer distinctive search terms: function names, command fragments, error
  text, ticket IDs, file names, and decision keywords.
- Use `history` when the original wording or tool output matters.
- Use `memory` when looking for approved, stable conclusions.
- Archive wrong facts instead of overwriting them when the old fact should no
  longer influence the agent and should remain traceable.
