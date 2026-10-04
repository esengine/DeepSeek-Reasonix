---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Session-start context example

1. This native v2 package contributes one `SessionStart` hook backed by
   `context/review.txt`. It runs no executable, shell, install script, or MCP
   server, and requires no compiler or package manager.
2. The reference suggests evidence-based review habits. It supplies model
   guidance; it does not enforce checks or establish a review verdict.
3. The manifest and reference are a starting point for a package author. Keep
   startup text short, relevant, and free of credentials or private data.

## Install and inspect

From the Reasonix repository root, preview and install a copy:

```sh
reasonix plugin install ./examples/review-context-kit --dry-run
reasonix plugin install ./examples/review-context-kit --yes
reasonix plugin doctor review-context-kit
reasonix plugin show review-context-kit
```

1. Preview MUST leave the package uninstalled. `show` after installation SHOULD
   report one hook, with no skills, MCP servers, theme, or extension runtime.
2. `doctor` checks package paths and configuration. It does not prove that a
   model has received the reference or followed it.
3. Start a new Reasonix session after changing installation or enablement.
   An already running session keeps its loaded hook configuration.

## Observe the contribution

1. Send a first prompt in a new session, for example: "Review the change in
   this workspace and report the checks you actually run."
2. The host reads the installed reference and adds its text to that turn's
   user message inside `<hook-context event="SessionStart">`. The reference
   stays out of the cache-stable system prefix and tool schema.
3. Send another prompt in the same session. The host does not append a new
   copy of this startup block. The earlier message remains in conversation
   history, subject to normal context management.
4. Create a new session with `/new`. The next user turn receives a fresh copy
   of the reference. Installation alone does not send a provider request.
5. The repository effect test records actual provider requests through
   `boot.Build`; it checks the newest user message on each turn, including
   session rotation. A live model's review quality requires separate evaluation.

```sh
go test ./internal/assembly/boot/ -run '^TestEffectReviewContextExampleLifecycle$' -count=1
```

## Edit and replace

1. Edit `context/review.txt` in the source package. A copy installation keeps
   its own files; source edits do not update that installed copy.
2. Preview and apply replacement, then start a new session:

```sh
reasonix plugin install ./examples/review-context-kit --replace --dry-run
reasonix plugin install ./examples/review-context-kit --replace --yes
reasonix plugin doctor review-context-kit
```

3. For local authoring, install a link instead. It keeps the source directory
   as the package root. Keep that directory available, and start a new session
   after edits so the changed text is read at session start.

```sh
reasonix plugin install ./examples/review-context-kit --link --dry-run
reasonix plugin install ./examples/review-context-kit --link --yes
```

4. Use copy and link in separate installations, or remove the current
   installation before switching mode.

## Disable and remove

```sh
reasonix plugin disable review-context-kit
reasonix plugin enable review-context-kit
reasonix plugin remove review-context-kit --yes
reasonix plugin list
```

1. Disable preserves the installed files but excludes the hook from a newly
   built session. Enable restores it for a new session.
2. Removal deletes the managed copy and registration. Removing a link keeps
   its external source directory.
3. Previously sent context remains in the old session's history. Start a new
   session when checking disable or removal; neither action erases messages
   that have already reached a provider.

## Troubleshooting

| Observation | Check |
| --- | --- |
| `doctor` reports a missing context file | Keep the readable regular file at `context/review.txt`, relative to the package root. |
| Editing the source has no effect | Replace the installed copy, or use a link; then start a new session. |
| No startup block reaches a new session | Check installation, enablement, and hook diagnostics before judging the model's answer. |
| Startup text appears in history on a later turn | Inspect the latest user message; old history is not a new injection. |
| The model makes an unsupported review claim | Verify source evidence and check output; reference text is guidance, not enforcement. |
