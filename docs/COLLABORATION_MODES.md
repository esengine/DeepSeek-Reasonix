# Collaboration modes and fact-driven execution

The desktop composer menu has two independent collaboration axes:

- **Plan mode**: draft a plan, then implement after approval.
- **Goal mode**: pursue one objective until it is complete, blocked, or stopped.

There is no automatic task mode. The one session role is the quality floor: standard (default) or delivery; facts can still raise it. Ordinary requests always
enter the executor. The dedicated planner runs only for an explicit Plan, an
approval boundary, or Goal start. Todos and sub-agents are model-chosen. The
host builds verification obligations from real tool actions.

The host evaluates cumulative effects, not just one tool call at a time. A
second production target upgrades sequential edits to multi-file preconditions.
Full verification requires every repository-declared check, or an unmistakably
project-wide verifier when no checks are declared. Reviews are accepted only
when their type, target coverage, and non-blocking verdict match the outstanding
obligation.

Plan, Goal, permission, sandbox, and the task contract are independent states.
Ask / Auto / Yolo keep their public meanings. The tool catalog stays stable so
the prompt cache stays warm. The Harness minimal preset is not a task
complexity mode.

## Plan mode

Plan mode is a workflow instruction, not a permission boundary. Writes stay
hard-blocked until the plan is approved, even under YOLO. `complete_step` waits
for approval.

## Goal mode

Goal mode keeps working inside the stated objective. Blocked Goals write into
the existing Goal state. Ordinary tasks that cannot satisfy a strict obligation
return a blocked explanation and must not look successful.
