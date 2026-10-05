---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Repository issue exercise

This native declarative package bundles an issue-fixing skill, two reference
files, and a deliberately failing Go fixture. It shows how to deliver a local
repository workflow as a complete package using the existing skill format.

## Prerequisites

1. Use a Reasonix installation that supports native v2 plugin packages.
2. Have Go 1.22 or later and a shell available for the fixture's test command.
3. Choose a model configured for text and tool calls in your Reasonix session.
   Installing the package does not validate that model's task quality.

## Install and run

From the Reasonix repository root, review and install a copy:

```sh
reasonix plugin install ./examples/issue-fix-kit --dry-run
reasonix plugin install ./examples/issue-fix-kit --yes
reasonix plugin doctor issue-fix-kit
reasonix plugin show issue-fix-kit
```

1. Copy `examples/issue-fix-kit/fixture/` to a new temporary directory. Keep the
   tracked fixture unchanged so the exercise remains reproducible.
2. In that copy, run `go test ./...` and observe the oversized-limit panic.
3. Open a Reasonix session in the temporary fixture. Invoke
   `/issue-fix-kit:issue-fix` with the task below.

```text
Use the bundled local issue scenario in this temporary fixture. Fix prefix.Take
so negative limits produce zero values and oversized limits produce the whole
input. Preserve the input, keep the tests, and provide a PR draft without
publishing it. Record the test result before and after the change.
```

## Acceptance and recovery

1. Confirm the installed package contains the skill and both files under
   `skills/issue-fix/references/`. Copy installation must retain those files.
2. Check the fixture's tests pass after the fix and the input is unchanged.
   Inspect the diff and compare the handoff with `references/delivery.md`.
3. If Go is missing or a check fails, retain the evidence and report the check
   as unavailable or failed. Doctor validates the package, not the task result.
4. Remove the test package with `reasonix plugin remove issue-fix-kit --yes`.
   Delete only your temporary fixture after preserving any wanted output.

This local exercise is not a public market submission. Installation and
resource-access tests do not prove that a live model will complete the task.
