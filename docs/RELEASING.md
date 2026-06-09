# Releasing

How Reasonix ships, who can ship what, and the canary-before-stable flow.

## Branch model: trunk + tags

- **`main-v2`** is the single development line (the v2 / 1.x trunk). Every PR merges here.
- **Production is a tag, not a branch.** A release is a tagged snapshot of `main-v2`:
  `v1.4.0` (CLI), `npm-v1.4.0` (npm), `desktop-v1.4.0` (desktop).
- **`v1`** is the archived 1.0/legacy line — maintenance only.
- **Hotfix** an already-released version by branching from its tag, fixing, and tagging again.

There is no separate "production" or "develop" branch by design — the canary channel
provides the pre-release buffer instead of a long-lived branch.

## Channels

| Surface | Stable | Pre-release buffer |
|---|---|---|
| npm | `latest` (0.x), `next` (1.x) | `canary` (`npm i reasonix@canary`) |
| Desktop | R2 `latest/` pointer | R2 `canary/` pointer (R2-only — never on the GitHub releases page) |

A canary build is isolated: it **never** moves `latest` / `next` / desktop `latest/`.
Testers opt in explicitly. (Desktop builds carry `-X main.channel=canary`; npm versions
ending in `-canary.N` publish under the `canary` dist-tag.)

## Who can release what

| Action | Who | Mechanism |
|---|---|---|
| **Cut a canary** | any maintainer (write access) | `workflow_dispatch`, runs free (open `canary` environment) |
| **Ship `next` / stable** | **esengine only** | stable publish jobs gate on the `release` environment — esengine must approve before anything goes public |

So a maintainer can dispatch a canary anytime, but a stable release — even one a
maintainer starts by pushing a tag — pauses in the Actions UI until **esengine approves**
the `release` environment deployment.

> Repo settings backing this: Environments → `release` has esengine as a required
> reviewer; `canary` has none. (Optional hardening: a tag ruleset restricting
> `v*`/`npm-v*`/`desktop-v*` creation to esengine, so maintainers can't even start a
> stable release.)

## The release loop

1. **Develop** — PRs land on `main-v2` (branch auto-deletes on merge).
2. **Cut a canary** before the intended release (e.g. heading for `1.4.0`):
   - Desktop: Actions → **Release desktop** → `channel: canary`, `base_version: 1.4.0`
   - CLI: Actions → **Release npm** → `base_version: 1.4.0`
   - Publishes `1.4.0-canary.N` to the desktop R2 `canary/` pointer (no GitHub release) and npm `@canary`.
3. **Test** — testers install `reasonix@canary` (CLI) or grab the desktop canary
   build from its R2 link, and report bugs.
4. **Fix** on `main-v2` via PRs; re-cut the canary as needed (`canary.N` bumps).
5. **Ship stable** when the canary is clean — push the three tags:
   ```sh
   git tag v1.4.0         && git push origin v1.4.0          # CLI binaries + Homebrew
   git tag npm-v1.4.0     && git push origin npm-v1.4.0      # npm -> next
   git tag desktop-v1.4.0 && git push origin desktop-v1.4.0  # desktop -> R2 latest/
   ```
   Each stable run **waits for esengine to approve the `release` environment** before publishing.
6. **Promote to default install** (optional, when 1.x should become the bare `npm i` target):
   ```sh
   npm dist-tag add reasonix@1.4.0 latest
   ```
7. **Next cycle** — the canary rolls on toward `1.5.0`.

## Notes

- Canary version numbers use the workflow `run_number`, so the desktop and CLI canary
  numbers differ (e.g. `canary.11` vs `canary.2`). Only monotonicity per channel matters.
- A stable `-rc` tag (e.g. `npm-v1.4.0-rc.1`) still ships under `next`, not `canary`.
- macOS canary self-update is manual (no notarization); testers download the canary
  build from its R2 link (canary is not on the GitHub releases page).
