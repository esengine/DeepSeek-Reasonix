---
name: issue-fix
description: Reproduce a repository issue, make the smallest correct fix, and draft a pull request from observed evidence.
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-01
---

# Issue fix

1. Use the repository selected for this session. Read its standing instructions
   and inspect its working tree before editing. Preserve existing changes.
2. Read the complete `references/delivery.md` beside this skill. Identify the
   issue's trigger, expected behavior, affected code, and acceptance checks.
   Resolve missing behavior before changing code; an issue title is not a spec.
3. Reproduce the trigger or trace its execution path. Record the command and
   observed failure. If reproduction is unavailable, state the limitation and
   the code or logs that support the proposed root cause.
4. Make a focused fix using the repository's existing patterns. Add or update
   a regression test for the trigger when feasible. Keep unrelated changes out
   of the patch and follow the repository's approval rules for wider changes.
5. Run the relevant checks and required repository gates. Report pass, fail,
   and not-run results separately. Review the final diff against the issue.
6. Draft the handoff using the reference format. Commit, publish a PR, or send
   a message only when the user has authorized that action.

The bundled `references/scenario.md` defines an optional local exercise. Use
it only when the user selects that fixture; it is not the issue to apply to
an arbitrary repository.
