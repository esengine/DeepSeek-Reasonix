# CLI host standing instructions

`--append-system-prompt-file PATH` lets a host supply standing instructions to
the interactive CLI or `reasonix run`. The file content enters the provider's
system message; the task remains a separate user message.

```sh
reasonix --dir /path/to/project \
  --append-system-prompt-file /absolute/path/host-instructions.md

reasonix run --dir /path/to/project --events-jsonl \
  --append-system-prompt-file /absolute/path/host-instructions.md \
  "implement the requested change"

reasonix run --dir /path/to/project --events-jsonl \
  --resume EXACT_SESSION_ID \
  --append-system-prompt-file /absolute/path/host-instructions.md \
  "continue the same task"
```

Use the exact session ID instead of a partial title or preview query for resume.
Supply the flag again on each new process, including resume. `EXACT_SESSION_ID`
above is a placeholder.

## Composition and lifetime

Reasonix first composes its built-in or configured system prompt, core policies,
and hierarchical project instructions such as `AGENTS.md` and `REASONIX.md`.
It then appends the file's exact UTF-8 content with one blank-line separator.
Full-trust extensions run afterward and retain their existing authority to
replace the system-prompt slot.

The option is held by the current process. It has no TOML setting and does not
rewrite user configuration, credentials, project instructions, or hook context.
Keep using the user's normal Reasonix home; hosts do not need a replacement
`REASONIX_HOME` to provide these instructions.

An absolute path is accepted directly. A relative path resolves against the
effective workspace after `--dir`. The resolved path is retained across TUI
model changes and `/reload`; each rebuild reads the file again and appends its
current content once. Resuming with the flag also reads the current file instead
of relying on an older instruction block in the saved conversation.

Native session history may retain the composed system prompt. Resuming without
the flag can therefore retain earlier instructions; it does not persist the
option or re-read the file. Hosts must supply the flag on every resume to apply
the current instructions.

The flag is parsed only before `--`. In
`reasonix run -- --append-system-prompt-file`, the flag-shaped token is ordinary
user task text.

## Validation and output

The file must be a readable, non-empty, regular UTF-8 text file. Missing files,
directories, empty files, unreadable files, and invalid UTF-8 fail before a model
turn. CLI validation failures exit with code `2`. Rebuilds validate the file again,
so deleting or corrupting it cannot silently remove the host's instructions.

Validation errors use fixed messages that omit both the path and its contents.
The option is not added to diagnostics or `--events-jsonl`; that stream retains
its existing content-free machine contract. The system-role instructions are
sent to the configured model provider as part of the ordinary conversation.

## Qualifying a release artifact

The black-box regression builds the real CLI, uses disposable homes and a local
fake provider, and checks run, exact resume, system-role composition, sanitized
errors, JSONL output, and a Unix PTY interactive turn:

```sh
go test ./cmd/reasonix -run TestAppendSystemPromptFileContract -count=1
```

To qualify an installed release instead of a source build, set
`REASONIX_TEST_BINARY` to its absolute executable path when running that command.
The Windows suite covers run/resume and validation; the PTY case runs on Unix.
The test reads the fixture's native session manifest for its exact ID because
native sessions currently omit `session_id` from `run_done`.
Hosts should verify a tagged release artifact before declaring a minimum
compatible version.
