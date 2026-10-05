---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-29
---

# Multi-file skill package example

This local example bundles a skill and a required reference file in a native
declarative plugin. The skill reads `references/format.md` when drafting a
release note. A direct market `SKILL.md` source would leave that file behind;
the plugin package copies the whole folder.

From the repository root, preview and install a **copy** in your configured
Reasonix home:

```sh
reasonix plugin install ./examples/release-note-kit --dry-run
reasonix plugin install ./examples/release-note-kit --yes
reasonix plugin doctor release-note-kit
reasonix plugin show release-note-kit
```

In a test repository:

1. Start a session and invoke `/release-note-kit:release-note` with a small
   set of known commits. Check that the draft cites only those commits, uses
   the reference's headings, and says when validation was not observed.
2. Check the installed package contains both
   `skills/release-note/SKILL.md` and
   `skills/release-note/references/format.md`. Preview alone does not prove
   the copied files or task result.

Remove the copied test package with
`reasonix plugin remove release-note-kit --yes`. This example is not a public
market submission or a verified release-note authoring service.
