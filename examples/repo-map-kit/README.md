---
owner: "@esengine"
backup: "@SivanCola"
status: active
reviewed: 2026-10-02
---

# Packaged repository orientation profile

This native v2 package contributes one manually invoked subagent profile,
`map`. It reads selected local files in an isolated child conversation and
returns an answer to the caller. It adds no hook, MCP server, extension runtime,
install script or production dependency.

1. The flat `agents/map.md` declares a description, a read-only tool ceiling
   and the method. Reasonix loads declared agent roots as manual subagent
   profiles; the qualified invocation is `/repo-map-kit:agent:map`.
2. The file omits a model override, so the normal host model selection applies.
   Use a configured model that supports tool calls. Live invocation uses that
   provider and its normal credentials and usage charges.

## Install and discover

From the Reasonix repository root:

```sh
reasonix plugin install ./examples/repo-map-kit --dry-run
reasonix plugin install ./examples/repo-map-kit --yes
reasonix plugin doctor repo-map-kit
reasonix plugin show repo-map-kit
reasonix subagent list
```

1. Preview must leave the package uninstalled. Installation copies the whole
   package into the configured Reasonix home.
2. `subagent list` must include `repo-map-kit:agent:map` as manual and read-only.
   `doctor` checks package paths; it does not verify an answer's quality.
3. Start a new session after changing package installation or enablement.
   Use the qualified invocation below in an interactive session. Packaged
   profiles are edited in their source package, rather than rewritten by the
   project/global profile editor.

## Run the bounded exercise

The fixture is source to inspect, not a command to execute. It needs no compiler
for this exercise. From the repository root, invoke the read-only runner:

```sh
reasonix subagent try repo-map-kit:agent:map --dir ./examples/repo-map-kit/fixture \
  "Explain LoadLines in loader.go, compare it with NOTES.md, and report what you did not verify."
```

`run` also honors this profile's `read-only: true`:

```sh
reasonix subagent run repo-map-kit:agent:map --dir ./examples/repo-map-kit/fixture \
  "Explain LoadLines in loader.go, compare it with NOTES.md, and report what you did not verify."
```

In an interactive session rooted at the fixture, use:

```text
/repo-map-kit:agent:map Explain LoadLines in loader.go, compare it with NOTES.md, and report what you did not verify.
```

1. Check that the answer explains newline splitting, whitespace trimming,
   blank-line removal and order preservation, citing `loader.go`.
2. `NOTES.md` deliberately claims blank lines and whitespace are preserved.
   A successful answer cites that note and the contradictory source.
3. The answer must list inspected files, say tests were not run and leave both
   files unchanged. This is repository orientation, not a review verdict.
4. Run a failure case asking for `missing.go`, which is absent. The answer must
   identify the missing input and avoid inventing its implementation.
5. The profile's tool ceiling excludes shell, writer and recursive delegation
   tools. The host also supplies its `complete_subtask` reporting protocol.
   Normal host access rules still govern readable files; naming a task directory
   is guidance, not a new per-profile filesystem access boundary.

## Edit, disable and remove

Edit the source profile, preview replacement, then apply it and start a new
session. Editing the source does not change an installed copy.

```sh
reasonix plugin install ./examples/repo-map-kit --replace --dry-run
reasonix plugin install ./examples/repo-map-kit --replace --yes
reasonix plugin disable repo-map-kit
reasonix plugin enable repo-map-kit
reasonix plugin remove repo-map-kit --yes
```

1. Disable preserves the copy but excludes the profile from newly built
   sessions. Enable restores it. Removal deletes the managed copy and registration.
2. For link-based authoring, remove the copy, preview with `--link --dry-run`,
   then apply with `--link --yes`. Keep the linked source available; removing
   the link registration keeps its external source directory.

## Verify the host path

```sh
go test ./internal/assembly/boot/ -run '^TestEffectRepoMapExampleLifecycle$' -count=1
```

1. The test copies this package through the real approved-plan installer,
   removes the temporary source and builds real controllers. It checks
   discovery, child prompt/task isolation, the read-only tool schema and actual
   fixture reads reaching the provider across install, disable, enable and removal.
2. The recording provider is scripted; this proves the host path, not that a
   live model correctly explains the exercise. The manual acceptance steps
   above are still needed for the selected model. No public market submission
   is claimed.

| Observation | Check |
| --- | --- |
| No profile in a fresh session | Confirm package enablement and `contributes.agents`; run `subagent list` in the target workspace. |
| A profile cannot be selected | Use the exact qualified name from `subagent list`, `/repo-map-kit:agent:map`. |
| An edit has no effect | Replace the copy or use a link, then build a new session. |
| The answer claims a build or edit | Check tool results; this profile has only reader tools. |
