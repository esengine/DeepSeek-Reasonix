---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-02
---

# Native prompt template example

1. This native v2 package contributes two Markdown prompt templates. It has
   no runtime, hook, MCP server, compiler, or installation script.
2. Templates become ordinary user turns when explicitly invoked. They supply
   model guidance; they do not run checks, limit tools, or grant permission.
3. Use a normally configured model for the manual exercise. Its normal account
   access and usage charges apply. Model output quality needs manual evaluation.

## Install and discover

From the Reasonix repository root, preview and install a copy:

```sh
reasonix plugin install ./examples/work-note-kit --dry-run
reasonix plugin install ./examples/work-note-kit --yes
reasonix plugin doctor work-note-kit
reasonix plugin show work-note-kit
```

1. Preview must leave the package uninstalled. The installed inventory must
   report two prompts and no runtime or executable contribution.
2. `contributes.prompts` names the `prompts` directory, not an individual file.
   A template name comes from its path: `handoff.md` becomes `handoff`, and
   `checks/summarize.md` becomes `checks:summarize`.
3. Start a new session after installation, or use `/reload` while idle. In
   Studio, use **Reload runtime** in **Settings → Extension → Installed**.
   The package prefix makes invocations distinct from other authors' templates.

## Run the exercise

Enter these in an interactive session:

```text
/work-note-kit:handoff lint-passed typecheck-passed integration-not-run
/work-note-kit:checks:summarize build unit-passed network-not-run
```

1. The handoff should preserve both passing checks, identify integration as
   unverified, and avoid claiming deployment or an independent review.
2. The check summary should use `build` as its label and preserve the full
   observations, including `network-not-run`. `$1` is the first whitespace
   separated argument; `$ARGUMENTS` includes all arguments, including the label.
3. `$$` emits one literal dollar sign. Templates are text substitution, not
   shell commands: quotes do not group arguments and no expression is evaluated.
4. Run `/work-note-kit:handoff` without arguments as a failure exercise. The
   template still reaches the model; a useful answer asks for missing facts.
   This is model behavior to assess, not a host-side input-validation guarantee.
5. Use qualified names. A short-name compatibility alias may be absent when a
   project command or another package owns that name.

## Edit, replace and remove

1. Edit a source template. A copy installation keeps its own files; source
   edits do not change that installed copy. Preview and apply replacement,
   then start a new session or reload while idle:

```sh
reasonix plugin install ./examples/work-note-kit --replace --dry-run
reasonix plugin install ./examples/work-note-kit --replace --yes
reasonix plugin disable work-note-kit
reasonix plugin enable work-note-kit
reasonix plugin remove work-note-kit --yes
```

2. Disable preserves the installed copy but excludes its templates from newly
   built sessions. Enable restores discovery. Removal deletes the managed copy
   and registration while keeping the original source.
3. For link-based authoring, remove the copy, preview using `--link --dry-run`,
   then install using `--link --yes`. Keep the linked source available and reload
   after edits. Removing a link preserves its external source directory.
4. Disable and removal do not erase a template already sent in session history.
   Use a fresh session when checking that a contribution is absent.

## Verify the host path

```sh
go test ./internal/assembly/boot/ -run '^TestEffectWorkNotePromptExampleLifecycle$' -count=1
```

1. The effect test uses the real approved-plan installer and `boot.Build`. It
   checks copied templates, source-edit isolation, replacement preview/application,
   disable/enable/removal, qualified naming beside a project-owned short command,
   and rendered arguments in actual provider user messages.
2. The provider is scripted. This proves host routing and substitution, not a
   live model's handoff quality or a public market publication.

| Observation | Check |
| --- | --- |
| No template in a fresh session | Check package enablement and the declared prompt directory; run `plugin show`. |
| The short name runs a different command | Use the full `/work-note-kit:<name>` invocation. |
| A source edit has no effect | Replace the installed copy, or use a link, then reload or build a fresh session. |
| The answer invents a check result | Compare it with the supplied observations; prompt guidance does not enforce truthfulness. |
