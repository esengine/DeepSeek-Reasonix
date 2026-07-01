# Slash Subagent Design

Status: implemented as a narrow slash-skill dispatch layer.

## Scope

`main-v2` already owns the durable subagent implementation:

- `runAs: subagent` skill metadata.
- The `run_skill` tool path.
- Built-in subagent tools such as `task`, `read_only_task`, and `read_only_skill`.
- Subagent transcript storage, parent-session ownership, continuation, model selection, tool scoping, and permission handling.

This PR only changes direct user invocation of slash skills:

- `/name args` for an inline skill keeps the existing behavior: render the skill body into the parent turn.
- `/name args` for a `runAs: subagent` skill runs through the existing `skill.SubagentRunner`.

Everything else stays out of scope:

- No `/subagents` management command.
- No separate task-center or transcript browser UI.
- No subagent-specific event or wire payloads.
- No desktop-only subagent runtime surface.

## Dispatch Order

Slash input keeps the same precedence as other commands:

1. Built-in slash commands run first.
2. Custom slash commands win over skills with the same name.
3. Skills are resolved last.

The skill path then branches by `runAs`:

```text
/name args
  -> custom command? render and start parent turn
  -> inline skill? render and start parent turn
  -> subagent skill? run existing SkillRunner with args as the task
```

## Subagent Slash Flow

For `runAs: subagent`, the controller handles the slash command directly:

1. Validate that a subagent runner exists for the session.
2. Validate that arguments are present. The child has no implicit user task beyond the slash arguments.
3. Start a guarded foreground controller operation so cancellation, busy state, autosave, and `TurnDone` stay consistent.
4. Attach the current parent session to the context with `agent.WithParentSession` and `jobs.WithSession`.
5. Attach response and reasoning language preferences to the context.
6. Compose the slash arguments so active goal, memory updates, background-job completions, plan-mode marker, and language blocks reach the child task.
7. Call `skill.SubagentRunner` with the resolved skill and composed task.
8. Record the visible slash command as the parent user message.
9. Record the child final answer as the parent assistant message.
10. Emit the final answer as ordinary `Text` and `Message` events, followed by the normal guarded `TurnDone`.

The parent conversation stores only the user slash command and final child answer. The child transcript remains isolated in the existing subagent store when the session supports persistence.

## UX Behavior

The user-facing contract is intentionally small:

- `/subagent-skill do the task` behaves like a foreground turn that returns a final answer.
- Empty arguments produce a notice instead of starting the child.
- Sessions without a configured subagent runner produce a notice.
- Inline skills are unchanged.
- Existing slash help, completion, and skill-management surfaces continue to come from the main skill registry.

Because the output is emitted as regular answer events, frontends do not need subagent-specific rendering to support this feature.

## Why Not A `/subagents` Command

The earlier design included a `/subagents` command, a task-center style browser, and extra event metadata. That overlapped heavily with the current mainline implementation and created frequent conflicts in the controller, TUI, desktop event bridge, and i18n catalogs.

The accepted shape is narrower: user-invoked `/name` subagents should reuse the existing skill runner. Management, browsing, and richer continuation UX can be designed separately on top of the mainline subagent store if needed.

## Split Plan

This PR intentionally avoids growing `controller.go` with a second subagent management surface. If future work needs richer subagent UX, split it into smaller follow-up PRs:

1. Extract slash skill dispatch from the controller into a focused helper.
2. Add read-only subagent listing APIs around the existing store.
3. Add frontend-specific browsing UI without changing the core runner path.
4. Add continuation commands only after the store-level API and UX are agreed.
