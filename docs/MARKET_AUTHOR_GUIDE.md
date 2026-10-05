---
owner: @SivanCola
backup: @esengine
status: active
reviewed: 2026-09-29
---

# Publish a community capability

## Purpose

This guide takes an author from a local skill or plugin to a tested submission in Studio's community market. A submitted package is a request for review, not an endorsement. Reviewers must pin the content of a version before the public market can install it.

## Steps

### 1. Make a local skill

From a test repository, create a project-scoped skill. Replace the example instructions with a method you can check on a real task.

```sh
mkdir -p .reasonix/skills/release-note
cat > .reasonix/skills/release-note/SKILL.md <<'SKILL'
---
name: release-note
description: Draft a release note from the changes the user selects.
---

# Release note

1. Ask which commits or changes belong in this release.
2. Read only those changes and group user-visible behavior separately from fixes.
3. Draft a short note with links to the selected changes.
4. Report anything that could not be verified; never invent a test result.
SKILL
reasonix doctor
```

Start a Reasonix session in that repository, run `/skills`, and invoke `/release-note` with a small, known set of changes. Check that the draft cites only those changes.

`reasonix doctor` reports missing descriptions and unavailable declared capabilities; fix them before publishing. [Skills](SKILLS.md) describes scope, `requires`, and invocation.

### 2. Package and preview it

A declarative plugin can collect one or more skills. Copy the skill into a separate package directory and give the package a manifest:

```sh
mkdir -p my-plugin/skills
cp -R .reasonix/skills/release-note my-plugin/skills/
cat > my-plugin/reasonix-plugin.json <<'JSON'
{
  "apiVersion": "reasonix.io/plugin/v2",
  "name": "release-note-kit",
  "version": "0.1.0",
  "description": "Draft release notes from selected changes",
  "contributes": { "skills": ["skills"] }
}
JSON
reasonix plugin install ./my-plugin --dry-run
reasonix plugin install ./my-plugin --link --yes
reasonix plugin doctor release-note-kit
reasonix plugin show release-note-kit
```

The dry run previews files and capabilities without writing them. `--link` is for local development: edits to the directory remain live source changes.

Choose the market kind from the files a user actually needs:

- A direct HTTPS `SKILL.md` source installs that file alone. Sibling
  `scripts/`, `references/`, templates, and other assets do not come with it.
- A local skill-directory install can copy sibling files. A GitHub directory
  submitted as a market **skill** can receive a content digest, but discovery
  installs only recognized Markdown skill files: `scripts/`, `references/`, and
  `assets/` directories are skipped, not copied with them.
- For a skill that needs sibling files, put its whole folder under a declarative
  plugin's `contributes.skills` directory as above. Publish the plugin from a
  commit-pinned GitHub tree URL. Preview and run the sample task from the copied
  installation, not only from the source checkout.

In Studio, use **Settings → Extensions → Installed → Runtime** to reload after changing linked code while idle, then repeat the sample task. The current running turn does not change.

For a copied package, install a new fixed source or use the existing update action. [Plugin Packages](PLUGIN_PACKAGES.md) describes the manifest, update, disable, export, and removal paths.

A code extension needs a Manifest v2 `runtime` block and a sidecar. Start from the [runnable Go example](../sdk/go/examples/starterextension/README.md) and preview its **FULL TRUST** block.

Follow [Extensions](EXTENSIONS.md) for build, reload, and protocol checks. SDK tags are published only with a corresponding product release; use a matching checkout until the tag you need exists.

### 3. Prepare a version for review

1. Record the package version, license, maintainer contact, supported platforms, required tools or credentials, a minimal input, an expected result, and one failure case in the source repository.
2. Test the example on the platforms you claim. Do not put secrets in a package, screenshot, or review description.
3. Commit and push the source. For a plugin or theme, use a GitHub tree URL with the full 40-character commit ID and package subdirectory, such as `https://github.com/OWNER/REPO/tree/FULL_COMMIT_ID/path/to/my-plugin`.
4. A local path or unpushed commit is not a public source. Use a direct HTTPS `SKILL.md` URL only for a self-contained skill; use a commit-pinned plugin package for a skill needing sibling files. MCP sources follow the market form's source rules.
5. Install from the exact source you intend to submit and repeat the sample task. Keep its input, expected and observed outcomes, environment, and source commit together. A download or dry run does not prove the task works.

### 4. Submit and maintain it

1. Sign in to Studio and open **Settings → Extensions → Discover → Publish**. Choose the correct kind, enter the name, version, fixed source, summary, description, repository URL, and tags. Read the form's source hint for the selected kind.
2. Choose **Private** to save a package visible only to your account. Test it under **My packages**; you can submit it for review there later. A public submission enters the review queue. Neither state means the package is already recommended or installable by others.
3. Follow the status under **My packages**. If review rejects the version, fix the source and submit a new version. Keep the tested commit and example evidence available to the reviewer.
4. For an update, increase the package version, repeat the local preview, doctor, and real-task check, then submit the new fixed source. Users keep their installed version until they choose an update.
5. To stop using a local linked plugin, disable it or run `reasonix plugin remove release-note-kit --yes`. Removal does not delete the external source directory.

A reviewer-recorded content digest lets users install without extra trust. An approved version without one requires an explicit **Trust and install** preview and confirmation.

The market UI is the publication path. The CLI commands above validate and manage local content; they do not submit a package or approve a review. Treat installation reports, votes, and popularity as discovery signals, not as proof that a scenario succeeded.

### What reviewers check

An admin approves a version against its pinned content hash. Review checks:

1. The source can be pinned.
2. The package name matches its content.
3. The package does not show suspicious behavior.
4. The package is not a duplicate.

Approval does not certify every use case or platform. Authors SHOULD keep their own task evidence current when publishing an update.
