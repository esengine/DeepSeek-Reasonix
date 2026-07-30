# Task Contracts and Pause Policy

<a href="./GUIDE.md">Guide</a>
&nbsp;·&nbsp;
<a href="./TASK_CONTRACT.zh-CN.md">Simplified Chinese</a>

Reasonix works best when nontrivial work is described as a **task contract**:
what the work is for, what action is requested, how the result should be
delivered, what boundaries must be preserved, and when the agent should pause.
Some prompt templates call the last section "Checkpoint"; Reasonix documents it
as "Pause policy" to avoid confusion with Checkpoints/Rewind snapshots.

This is intentionally not a larger role prompt. Strong coding agents usually do
not need step-by-step thinking instructions. They need clear boundaries and
acceptance criteria.

## Template

```text
Context:
I am working on [larger task].
The target audience/user is [who].
This result should help them [achieve what outcome].

Request:
Please complete [one clear action].

Output format:
Return the result as [specific structure].
It must include [required sections].
Keep it within [length or scope].

Constraints:
Do not [bad assumption].
Do not [out-of-scope content].
Do not [low-quality output shape].
If information is missing, mark uncertainty explicitly.

Pause policy:
Unless the next step involves an irreversible or externally visible operation,
a scope change, or information only I can provide, keep working and report back
after the task is complete.
```

## How Reasonix Uses It

- **Normal chat** can use the template directly for one-off work.
- **Goal mode** treats the goal as a task contract and keeps working until the
  request, output format, constraints, and verification expectations are
  satisfied.
- **Plan mode** is the right choice when you want the model to draft and confirm
  a plan before implementation. It is a workflow instruction, not a read-only
  permission boundary.
- **Tool approval** remains separate: file writes, shell commands, publishing,
  credentials, and external effects still follow the configured approval policy.
- **Checkpoints/Rewind** are file and conversation snapshots. The task contract's
  pause policy is about when the agent should ask the user before continuing.

The Goal-mode task contract rides the provider-visible user turn. It does not
rewrite the cache-stable system prompt, memory prefix, or tool schemas.

## Example

```text
/goal Context:
I am improving the desktop composer.
The target user is someone doing repeated code review sessions.
This should help them avoid accidental interruptions.

Request:
Make the slash-command menu keep keyboard focus while suggestions are open.

Output format:
After implementation, summarize changed files and verification results.

Constraints:
Do not change the Wails JSON contract.
Do not refactor unrelated composer state.
If browser verification cannot run, say why.

Pause policy:
Unless the next step requires a product decision, a public push, or credentials,
continue through implementation and verification before reporting back.
```
