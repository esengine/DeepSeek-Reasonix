# Collaboration modes and execution facts

The composer offers Plan and Goal. There is no selectable quality floor.
Ordinary requests end when the model ends normally; pending todos and missing
checks do not cause host-generated continuation.

## Plan

Plan previews work for approval. Before approval, the host blocks writes,
including Yolo, proxy tools and subagents. After approval, the model implements
the plan and feedback, updates todos, and judges completion. Acceptance notes
are task instructions; no sequential evidence signoff is required.

## Goal

An explicitly activated Goal continues after a normal turn when the model
reports `continue` or provides no report. `update_goal(complete)` commits the
model's completion declaration at a normal end boundary; `blocked` stops
continuation immediately. There is no independent completion evaluator.
Restored and forked Goals need explicit activation. Cancellation, user input,
permission waits, errors and explicit budgets retain their boundaries.

Plan, Goal, permission, sandbox, and the task contract are independent states.
Read only, Workspace write, and Full access keep their public meanings. The tool
catalog stays stable so the prompt cache stays warm. The Harness minimal preset
is not a task complexity mode.

## Permissions and results

Read only / Workspace write / Full access, sandbox restrictions and explicit
prohibitions remain action controls. They do not certify task quality. Tool results retain actual failures,
exit codes and interruptions. Checks that precede later edits are stale.
Model completion declarations and execution facts are distinct; unfinished
todos are not automatically marked complete.

Plan mode is a workflow instruction, not a permission boundary. Writes stay
hard-blocked until the plan is approved, even under Full access. `complete_step` waits
for approval.

See [execution semantics and migration](EXECUTION_MODEL_SIMPLIFICATION.md) and
[task instructions](TASK_CONTRACT.md). Tool ordering and serialization stay
stable within a version; historical provider-visible messages are not rewritten.
