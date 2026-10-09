---
name: release-note
description: Draft a release note from user-selected changes, citing only verified behavior.
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-09-29
---

# Release note

Ask which commits, pull requests, or files belong in the release and who will
read the note. Read the selected changes and the complete
`references/format.md` file next to this skill before drafting. Do not infer
user-visible behavior from a title alone.

Use the format in that reference. Link each claim to the selected change when
a link is available. Keep fixes separate from new behavior. If a migration or
compatibility effect is uncertain, mark it for confirmation rather than
guessing.

Report tests as passed only when their result was observed; otherwise use the
reference's unverified wording.
