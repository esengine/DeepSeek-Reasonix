# AutoResearch Runtime Design

## Context

Reasonix already has Goal mode and AutoResearch instructions. When a goal looks
long-running, `activeGoalBlock` injects an AutoResearch protocol that asks the
model to create `.reasonix/autoresearch/<task-id>/` and maintain files such as
`task_spec.json`, `progress.json`, `findings.jsonl`, `directions_tried.json`, and
`heartbeat.jsonl`.

The current behavior is useful, but the durable state is mostly prompt-driven.
The host does not create the task directory, validate schemas, compute
`stale_count`, require pivots, expose a structured status API, or provide a real
resume mechanism. This design upgrades AutoResearch from a prompt convention to
a host-managed runtime.

## Goals

- Host creates and owns the AutoResearch task id and directory layout.
- Host validates task state with typed schemas.
- Host records heartbeat, progress, findings, directions tried, and iteration log.
- Host computes stale and pivot pressure from accepted evidence and direction
  repetition.
- Goal completion is blocked until required success criteria have evidence.
- Existing AutoResearch task ids can be resumed by the controller.
- The desktop/API layer can query AutoResearch status without parsing prompt text.

## Non-Goals

- No large desktop panel in the first implementation.
- No autonomous background daemon. AutoResearch advances only through normal Goal
  turns.
- No parallel writable sub-agent redesign in this feature.
- No network, publish, payment, credential, or destructive-operation bypass.
  Existing Reasonix gates still apply.

## Proposed Architecture

Add a new package:

```text
internal/autoresearch/
  task.go
  store.go
  schema.go
  summary.go
  readiness.go
```

The package is responsible for all filesystem state under:

```text
.reasonix/autoresearch/<task-id>/
  state/
    task_spec.json
    progress.json
    directions_tried.json
    findings.jsonl
    iteration_log.jsonl
  logs/
    heartbeat.jsonl
```

The controller remains the owner of Goal lifecycle. AutoResearch state is a
durable sidecar attached to a running Goal when research mode is on or auto
research is triggered.

## Core Types

`TaskSpec`:

```json
{
  "task_id": "20260629-153000-debug-lag",
  "goal": "Find the root cause of UI event-loop lag and verify the fix",
  "scope": ["desktop/frontend", "desktop"],
  "non_goals": [],
  "allowed_operations": {
    "write": true,
    "network": false,
    "publish": false
  },
  "success_criteria": [
    {
      "id": "root_cause",
      "description": "A reproducible root cause is identified",
      "required": true,
      "evidence_ids": []
    }
  ]
}
```

`Progress`:

```json
{
  "status": "running",
  "iteration": 4,
  "current_direction": "profile markdown rendering",
  "stale_count": 1,
  "pivot_count": 0,
  "blocked_reason": "",
  "updated_at": "2026-06-29T15:30:00Z"
}
```

`Finding` JSONL entries:

```json
{
  "id": "f1",
  "kind": "test",
  "summary": "A markdown render benchmark reproduces the lag",
  "source": "command",
  "command": "pnpm --dir desktop/frontend test",
  "paths": ["desktop/frontend/src/components/MarkdownRenderer.tsx"],
  "accepted": true,
  "created_at": "2026-06-29T15:30:00Z"
}
```

`DirectionTried` entries record normalized direction fingerprints so the host can
detect repeated work:

```json
{
  "fingerprint": "profile-markdown-rendering",
  "summary": "Profile markdown rendering",
  "first_seen_iteration": 2,
  "last_seen_iteration": 4,
  "count": 2
}
```

## Store API

The first implementation should expose a small host API:

```go
type Store struct { /* workspace root + autoresearch root */ }

func (s *Store) CreateTask(goal string, opts CreateOptions) (*Task, error)
func (s *Store) LoadTask(taskID string) (*Task, error)
func (s *Store) ResumeFromGoalText(goal string) (*Task, bool, error)
func (s *Store) AppendHeartbeat(taskID string, h Heartbeat) error
func (s *Store) AppendFinding(taskID string, f Finding) error
func (s *Store) RecordDirection(taskID string, d Direction) (*Progress, error)
func (s *Store) UpdateProgress(taskID string, patch ProgressPatch) (*Progress, error)
func (s *Store) ValidateTask(taskID string) (*ValidationReport, error)
func (s *Store) Readiness(taskID string) (*ReadinessReport, error)
func (s *Store) Summary(taskID string) (*Summary, error)
```

All writes should be atomic: write to a temp file in the same directory, fsync
where practical, then rename. JSONL appends should validate each entry before
writing.

## Controller Integration

When Goal mode starts:

1. If research mode is off, behavior is unchanged.
2. If AutoResearch is on, the controller creates a task unless the goal contains
   an explicit `.reasonix/autoresearch/<task-id>/` path.
3. If an explicit task path exists, the controller loads and validates that task.
4. The active goal state stores `AutoResearchTaskID`.

Before each AutoResearch turn:

- The controller appends a heartbeat with `status=starting_turn`.
- The composed user input includes a concise host-generated summary:
  task id, status, iteration, current direction, stale count, pivot count,
  blockers, open success criteria, and next required runtime action.
- The static prompt still explains the protocol, but host state is authoritative.

After each turn:

- The controller appends a heartbeat with `status=turn_done`.
- If tools ran, it records basic iteration metadata.
- If no accepted evidence was recorded, or the same direction repeated, the host
  increments `stale_count`.
- At `stale_count >= 2`, the next turn summary requires a structural pivot.
- At `stale_count >= 4`, the goal is blocked unless the agent asks for the
  smallest external input needed.

The model may still write detailed notes, but host-owned JSON files are the
source of truth.

## Completion Gate

When the model emits `[goal:complete]`, the controller runs AutoResearch
readiness before normal Goal completion:

- `task_spec.json` and `progress.json` must exist and validate.
- Every required success criterion must have at least one accepted evidence id.
- Evidence ids must resolve to entries in `findings.jsonl`.
- If code was changed, there must be accepted verification evidence or an
  explicit accepted reason why verification could not run.
- If `progress.status` is blocked, completion is rejected.
- If `stale_count > 0`, completion is allowed only when the final iteration added
  accepted evidence that addresses the stale direction.

Failure returns a concrete intercept message to the model listing missing
criteria and required next actions.

## Resume Behavior

AutoResearch resume has two paths:

- Explicit: a goal or prompt includes `.reasonix/autoresearch/<task-id>/`.
- Session-sidecar: the persisted Goal state contains `AutoResearchTaskID`.

On resume, the host validates the task. If state is corrupt, it blocks execution
with a repair message instead of silently asking the model to infer state.

## Desktop/API Surface

The first UI implementation should make AutoResearch visible without turning the
chat surface into a project-management app. It should use the existing desktop
layout patterns: status bar for compact state, side panels for inspectable
details, and transcript cards for turn-local events.

Host methods:

- `AutoResearchStatus(taskID string)`
- `AutoResearchList()`
- `AutoResearchCurrent()`
- `AutoResearchFindings(taskID string, limit int)`
- `AutoResearchOpenTask(taskID string)`

The status payload should include:

- task id
- goal
- status
- iteration
- current direction
- stale count
- pivot count
- last heartbeat
- finding count
- open success criteria
- blocker

`AutoResearchOpenTask` opens `.reasonix/autoresearch/<task-id>/` in the
workspace panel or OS file browser, matching existing workspace reveal behavior.

## Deferred Desktop UI Design

The runtime PR exposes the desktop API and compact tab metadata first. The
default frontend tool/status surface intentionally stays unchanged until the UI
entry points below are implemented and reviewed as a separate product decision.

### Entry Points

AutoResearch should appear in three places:

1. Status bar chip: a compact always-visible indicator when the active tab has a
   running or resumable AutoResearch task.
2. Context/side panel section: an inspectable task summary for the active tab.
3. Transcript event cards: lightweight markers for task creation, pivot required,
   blocked, resumed, and completed.

This keeps the primary chat workflow intact while making durable research state
visible and recoverable.

### Status Bar Chip

Add an `autoresearch` status item to the existing status bar item system. It is
hidden when no AutoResearch task is active for the current tab.

Display states:

- `Research 4` for running iteration 4.
- `Pivot` when `pivot_required` is true.
- `Blocked` when status is blocked.
- `Done` briefly after completion.

The chip should include an icon, short label, and tooltip. The tooltip contains:

- task id
- current direction
- stale count
- open criteria count
- last heartbeat age

Clicking the chip opens the AutoResearch detail panel.

### Detail Panel

Add a compact AutoResearch section to the right-side context/workspace area. The
panel should be dense and operational, not decorative.

Header:

- task id
- status
- iteration
- last heartbeat
- open task folder button

Summary rows:

- goal
- current direction
- stale count
- pivot count
- blocker

Success criteria list:

- criterion description
- required/optional marker
- evidence count
- status: open, satisfied, blocked

Findings list:

- newest accepted findings first
- kind badge: command, file, test, benchmark, manual, review
- summary
- source command/path if present
- created time

Controls:

- Resume: starts or continues `/goal --research .reasonix/autoresearch/<task-id>/`
  for the active tab when not running.
- Pause: clears active Goal continuation without deleting task state.
- Open Folder: reveals the task directory.
- Copy Task ID: copies the task id.

The first implementation can omit inline editing of task spec fields. Task state
is owned by the host and model workflow; UI edits would need validation and a
separate audit path.

### Transcript Cards

Emit lightweight notices or typed events for important AutoResearch lifecycle
changes:

- task created
- task resumed
- heartbeat failed
- pivot required
- readiness blocked completion
- task completed
- task blocked

The transcript should not render the full runtime summary every turn. It should
show only meaningful lifecycle changes, because the full summary is already
available in the detail panel and injecting it visually every turn would add
noise.

### Frontend State Flow

Extend bridge/types with:

```ts
interface AutoResearchStatusView {
  taskId: string;
  goal: string;
  status: "running" | "blocked" | "complete" | "stopped" | "invalid";
  iteration: number;
  currentDirection: string;
  staleCount: number;
  pivotCount: number;
  pivotRequired: boolean;
  lastHeartbeatAt: string;
  findingCount: number;
  openCriteria: AutoResearchCriterionView[];
  blocker: string;
  taskPath: string;
}
```

`MetaForTab` should include only the small active-task summary needed to render
the status chip. The heavier findings list should be loaded on demand through
`AutoResearchFindings`, so normal chat turns do not pull large JSONL data into
frontend state.

Refresh strategy:

- Refresh current AutoResearch status after `turn_done`.
- Refresh when switching tabs.
- Refresh when receiving an AutoResearch lifecycle event.
- Do not poll every second. The heartbeat is persisted for durability, not for a
  live dashboard animation.

### Empty and Error States

- No active task: hide the chip and show no panel section by default.
- Invalid state: show an `Invalid` status with the exact validation error and an
  Open Folder action.
- Missing task folder on resume: show a blocked state and keep the Goal from
  silently continuing.
- Stale state: show the pivot requirement prominently but do not mark it as an
  error.

### Accessibility and Layout

- Use existing button, tooltip, panel, and status bar patterns.
- Keep the status chip width stable so the status bar does not shift during
  streaming.
- Long goals and directions should wrap in the panel but truncate in the chip
  tooltip label.
- Findings should be keyboard navigable and copyable.
- Use icons only where the existing design system already uses them; avoid a
  marketing-style card layout.

## Error Handling

- Missing task directory: create only when starting a new task; otherwise return
  a clear resume error.
- Corrupt JSON: block AutoResearch continuation and surface the exact file.
- Schema mismatch: return validation errors with paths and fields.
- Failed heartbeat write: warn and continue for non-critical writes, but block
  completion if state cannot be validated.
- Concurrent writes: serialize per task id inside the store.

## Testing

Unit tests:

- task id generation is stable in shape and collision-safe
- store creates the expected directory layout
- schema validation rejects missing required fields
- JSONL append rejects invalid entries
- direction repetition increments stale count
- pivot threshold produces a pivot requirement
- readiness blocks missing evidence
- readiness accepts complete criteria with accepted findings
- resume loads explicit task id from goal text

Controller tests:

- AutoResearch goal creates task state
- active goal block includes host-generated summary
- every turn appends heartbeat
- `[goal:complete]` is intercepted when readiness fails
- explicit `.reasonix/autoresearch/<task-id>/` resumes existing state

Desktop/API tests:

- app method returns current AutoResearch status for the active tab
- tab metadata includes only compact AutoResearch summary
- findings are loaded on demand and capped by limit
- status chip hides when no task is active
- status chip opens the detail panel
- detail panel renders running, pivot, blocked, invalid, and complete states
- Open Folder calls the existing reveal/open path behavior
- tab switch refreshes the visible AutoResearch summary

## Rollout

Phase 1: implement `internal/autoresearch` store, schemas, summary, readiness,
and tests.

Phase 2: integrate with Goal controller create/resume/heartbeat/summary and
completion intercept.

Phase 3: add desktop/API status methods, status bar chip, detail panel, transcript
lifecycle cards, and frontend tests.

Phase 4: optionally let tools or a dedicated host tool record structured
findings directly, reducing reliance on model-authored JSON.

## Compatibility and Cache Impact

This design should not change provider-visible tool schemas in phase 1. The
active goal prompt changes only when AutoResearch is active. Cache impact is
therefore low for ordinary sessions and medium for AutoResearch sessions because
the injected runtime summary changes each turn.

No existing `.reasonix/autoresearch` task should be deleted or rewritten without
validation and explicit migration logic.
