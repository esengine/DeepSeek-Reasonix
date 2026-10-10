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

For a first exercise with fixed inputs, use [the selected change evidence](fixture/change-evidence.md).
The audience is contributors to the prefix example; select RN-01 only. After
reading the installed skill and its format reference, enter this request in a
normally configured Reasonix session:

```text
/release-note-kit:release-note Draft a release note for contributors to the prefix example, using RN-01 in fixture/change-evidence.md as the selected change. Cite the source evidence and observed checks, leave the unselected idea out, and identify missing release/version approval. Do not publish the draft or run additional commands.
```

1. Resolve the evidence path relative to this installed package, or copy the
   file into the test repository and provide its full path. It is a human-read
   exercise asset, not an automatically discovered command or skill resource.
2. Compare the answer with [the worked draft](fixture/example-release-note.md)
   only after producing your own answer. The record is a local example's
   observed patch and checks; it is not a Reasonix release or a published PR.
3. Codex checked and revised the worked draft against the installed skill,
   complete format reference and fixed evidence. Native Reasonix live-model
   output quality and independent human approval remain unverified. Installation and a scripted provider's
   reference-read test establish different facts from authoring quality.
4. For an unverified-input exercise, copy the evidence into a test directory,
   remove its observed-check section and explicitly say no results were
   supplied. The answer should use the format reference's unverified wording
   and preserve the change's scope. Do not invent replacement test results.

Remove the copied test package with
`reasonix plugin remove release-note-kit --yes`. This example is not a public
market submission or a verified release-note authoring service.
