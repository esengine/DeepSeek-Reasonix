---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-10-01
---

# Compatible command package example

1. This self-contained package uses a Claude-format manifest and one Markdown
   command. It declares no process, hook, MCP server, credential or dependency.
   Reasonix loads the command as a prompt template; invoking it starts a model
   turn, whose normal tools and approval rules still apply.
2. Preview and install a copy from the repository root. These commands write to
   your configured Reasonix home only after the explicit install:

   ~~~sh
   reasonix plugin install ./examples/command-notes-kit --dry-run
   reasonix plugin install ./examples/command-notes-kit --yes
   reasonix plugin doctor command-notes-kit
   reasonix plugin show command-notes-kit
   ~~~

3. Start a fresh session in a test repository. Studio's composer command menu
   and the CLI completion use the qualified command name. Run:

   ~~~text
   /command-notes-kit:note ISSUE-7 verified change; validation not run
   ~~~

   The model receives the full request, `ISSUE-7` as the first argument,
   `verified` as the second, and a literal `$`. Arguments are split on
   whitespace; quotes do not group them. Check the resulting note cites only
   the supplied facts and says validation was not run.

   This output requires a model; the offline regression verifies expansion
   and delivery, not note quality.
4. Use the qualified name even when a short alias is available. A project
   `.reasonix/commands/note.md` owns `/note`; it does not hide
   `/command-notes-kit:note`. The model can list and expand the template with
   the existing `slash_command` tool, which returns instructions rather than
   executing them as a separate task.
5. To try the Codex-format mapping, make a separate copy of this directory and
   rename `.claude-plugin` to `.codex-plugin`, keeping `plugin.json` and
   `commands/note.md` unchanged. Install that copy instead, with `--replace`
   after reviewing its dry run.

   Keep one manifest format per test directory. The regression installs both
   formats independently from these same files.
6. Disable, re-enable and remove the test package. Start a fresh session after
   each change to check the command menu and invocation:

   ~~~sh
   reasonix plugin disable command-notes-kit
   reasonix plugin enable command-notes-kit
   reasonix plugin remove command-notes-kit --yes
   ~~~

   Disabling leaves the copied package on disk. Removing deletes that copy,
   preserves the original source and leaves project-authored commands alone.
   This example makes no public market submission or cross-product runtime
   compatibility claim.

## Compare with a worked note

1. [Worked note](example-output/note.md) uses the fixed `ISSUE-7` request
   above. It separates the complete input from `$1` and `$2`, preserves
   validation as not run, and avoids inventing a changed file or test result.
2. In a fresh session, try the missing-facts case:

   ```text
   /command-notes-kit:note ISSUE-7
   ```

   Compare with the reference's request for missing change and validation
   evidence. This is model behavior to assess, not host-side input validation.
3. The references are implementer-written, checked against `commands/note.md`,
   and stored outside `commands`. They add no invocation and are not captured
   live-model output. Record the chosen model and actual answer separately.

## Verify the host path

```sh
go test ./internal/assembly/boot/ -run '^TestEffectInstalledCompatibleCommandLifecycle$' -count=1
```

1. The test installs Claude and Codex manifest mappings independently. It
   checks real approved-plan copy, qualified discovery, argument expansion,
   project short-name ownership, provider user-message delivery, disable,
   enable and removal without changing the stable prefix or tool schema.
2. Its provider is scripted. This establishes Reasonix host behavior, not a
   live model's note quality or acceptance in another product.
