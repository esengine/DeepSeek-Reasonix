# Releasing

How Reasonix ships, who can ship what, and the Preview-before-Stable flow.

## Branch model: trunk + tags

- **`main-v2`** is the single development line (the v2 / 1.x trunk). Every PR merges here.
- **Production is a tag, not a branch.** A release is a tagged snapshot of `main-v2`:
  `v1.4.0` (CLI), `npm-v1.4.0` (npm), `desktop-v1.4.0` (desktop).
- **`v1`** is the archived 1.0/legacy line — maintenance only.
- **Hotfix** an already-released version by branching from its tag, fixing, and tagging again.

There is no separate "production" or "develop" branch by design. Desktop
Preview provides the fast pre-release buffer instead of a long-lived branch.

## Channels

| Surface | Stable | Pre-release buffer |
|---|---|---|
| Native CLI | GitHub Release `vX.Y.Z` + Homebrew | GitHub prerelease `vX.Y.Z-preview.N` (never Homebrew or GitHub Latest) |
| npm | `latest` (current 1.x stable) | `next` (rc), `canary` (`npm i reasonix@canary`) |
| Desktop | R2 `latest/` pointer + release gateway | R2 `preview/` pointer + release gateway proxy (never on the GitHub releases page) |

Native CLI and Desktop have exactly two user-facing channels:

- **Preview** is the opt-in, fast channel, normally cut every 1–2 days. Native
  CLI uses an immutable protected `vX.Y.Z-preview.N` tag and a GitHub
  prerelease; Desktop builds carry `-X main.channel=preview`, use the same
  version shape, and move only the desktop `preview/` pointer.
- **Stable** is the weekly channel. It publishes the native CLI GitHub Release
  and Homebrew cask and moves only the desktop `latest/` pointer.

Both channels are public product builds. Homebrew remains Stable-only because
it has no separate prerelease channel. On Windows, Desktop Preview and Stable
use the same verified publisher identity and the SignPath `release-signing`
policy. Certificate trust must not be used to communicate release quality.
`test-signing` is reserved for internal CI/signing validation and must never
publish the public Preview pointer.

An RC is not a third user-facing channel. If a weekly candidate needs a freeze,
use a surface-specific `vX.Y.Z-rc.N` or `desktop-vX.Y.Z-rc.N` tag as an
internal candidate checkpoint. Neither moves a rolling pointer or Homebrew.
CLI and Desktop RCs reuse the reviewed `X.Y.Z` Stable notes instead of creating
a third public changelog identity, so prepare and merge that Stable record
before cutting the RC. npm retains its separate `next` and `canary` dist-tags.

### Desktop channel compatibility

| Input/client state | Effective behavior |
|---|---|
| No `desktop.update_channel` setting | Stable |
| `stable` | Stable |
| `preview` | Preview |
| Legacy `canary`, `beta`, or `next` setting | Normalized as Preview; future writes persist `preview` |
| Unknown value | Stable (fail closed) |
| Older client polling `canary/latest.json` | Receives the mirrored Preview pointer |
| New client polling Preview | Tries `preview/` first, then legacy `canary/` during migration |

## Who can release what

| Action | Who | Mechanism |
|---|---|---|
| **Ship Preview** | release-tag creator + configured reviewer | prepare notes for the exact `X.Y.Z-preview.N` version, then create and push its protected CLI tag; a minimal relay dispatches **Release preview** on protected `main-v2`, which pauses once on `canary` and publishes aligned CLI, Desktop, npm, and changelog identities |
| **Ship stable** | release-tag creators + one configured reviewer | atomically push the three stable tags; a minimal tag relay dispatches **Release stable** on protected `main-v2`, which requests one GitHub `release`-environment approval before every channel publishes |
| **Ship a standalone RC** | release-tag creators + one configured reviewer | prepare the reviewed `X.Y.Z` Stable notes, then push the surface-specific prerelease tag; a minimal relay dispatches the standalone workflow on protected `main-v2`, reuses those notes, and requests one `release` approval |

Preview remains operationally fast, but its public artifacts are not an
unreviewed or test-certificate path. The unified Preview event pauses once at
the legacy `canary` environment approval, then uses the production SignPath
policy. A stable release pauses once in
**Release stable** until a configured reviewer approves the `release`
environment. The jobs then continue without another human approval: SignPath
verifies the trusted GitHub origin, scans and signs every installed executable,
rebuilds the packages, and signs the outer installers.

> Repo settings backing this: Environments → `release` and `canary` have the
> same release owners as required reviewers, and the release-tag ruleset restricts
> `v*`/`npm-v*`/`desktop-v*` creation, update, and deletion. Only the
> orchestrator and standalone RC/recovery paths reference the protected
> environment.

The tag-triggered workflows contain no build or signing steps. They relay only
the immutable candidate tag to the current workflow on protected `main-v2`.
The reusable publishers require that protected control plane, while the
orchestrator resolves the three release tags to one immutable candidate SHA and
uses that SHA only for build and publication checkouts. Recovery follows the
same model for an older tag on `main-v2` history. Every publisher revalidates
its remote release tag immediately before publication. An unprotected branch
cannot claim that it already passed the approval job.

Repository `write` access remains a privileged role because repository-level
Actions secrets are available to workflows on repository branches. Production
Windows signing has an additional provider-side boundary: SignPath accepts the
trusted `.github/workflows/release-stable.yml`,
`.github/workflows/release-preview.yml`, and
`.github/workflows/release-desktop.yml` build definitions only when their
origin branch is exactly `main-v2`. Never broaden that policy to `**` or a
tag-shaped wildcard. Other publication credentials should move to protected
environment secrets or provider-side OIDC/trusted-publishing policies when
strict separation from repository writers is required.

## The release loop

1. **Develop** — PRs land on `main-v2` (branch auto-deletes on merge).
2. **Prepare the release notes without creating a release-only PR by default** —
   Actions → **Prepare release notes**. Enter the exact intended version
   (`X.Y.Z` for Stable or `X.Y.Z-preview.N` for Preview), the previous release
   tag when needed, and the number of an existing release-bound PR when one is
   available. Every exact Stable and Preview identity gets its own reviewed
   catalog record. Do not create an `X.Y.Z-rc.N` catalog record: standalone
   CLI/Desktop RCs are internal checkpoints and reuse the `X.Y.Z` Stable
   record. The reusable PR must be open, target `main-v2`, come from a
   branch in this repository, and already include the latest `main-v2`. The
   workflow commits the generated notes onto that branch, so product changes and
   their release copy share one PR and one review surface. The added commit still
   reruns that PR's required checks.

   Leave `target_pr` empty only when there is no suitable PR, the candidate PR
   comes from a fork that the repository token cannot update, or the release copy
   needs independent editorial review. In that fallback, the workflow opens or
   updates the dedicated `release-notes/vX.Y.Z` PR as before.

   The workflow sends only public merged-PR metadata to DeepSeek, creates
   equivalent English and Chinese product notes, validates their structure and
   citations, and includes the rendered draft in the workflow summary. Review
   the catalog diff like product copy. Once merged, the same entry drives
   the exact `/changelog/vX.Y.Z/` or `/changelog/vX.Y.Z-preview.N/` page and the
   corresponding release surfaces; the desktop app links directly to that exact
   web history from Settings → Updates. A reviewed record stays hidden until the
   all-surface postflight uploads its matching immutable `release-event.json`.
   Missing or mismatched notes block publication.
3. **Cut Preview** during the intended release cycle (e.g. heading for `1.4.0`):
   - First merge the reviewed notes for the exact Preview identity, then create
     and push its protected tag:
     ```sh
     git tag v1.4.0-preview.1
     git push origin v1.4.0-preview.1
     ```
   - The tag relay dispatches **Release preview** on protected `main-v2`. After
     one `canary` approval and a zero-publication SignPath preflight, it publishes
     CLI `v1.4.0-preview.1`, Desktop `1.4.0-preview.1`, npm
     `1.4.0-canary.1`, and the exact Preview changelog as one event.
   - Desktop advances only R2 `preview/` (and mirrors `canary/` for older
     clients); no Desktop Preview appears on the GitHub releases page. npm
     advances only `@canary`; Homebrew and Stable pointers remain untouched.
4. **Test** — native CLI testers download the immutable GitHub prerelease;
   desktop users opt into Preview in Settings → Updates; npm CLI testers
   install `reasonix@canary`.
5. **Fix** on `main-v2` via PRs; re-cut Preview as needed (`preview.N` bumps).
   Re-run **Prepare release notes** after material fixes. Reuse the still-open
   release-bound PR when possible; otherwise the workflow updates the same
   dedicated release-notes branch and PR without publishing anything.
6. **Ship stable** when Preview is clean and the release-notes PR is merged —
   create the three tags locally, then push them atomically:
   ```sh
   git tag v1.4.0
   git tag npm-v1.4.0
   git tag desktop-v1.4.0
   git push --atomic origin v1.4.0 npm-v1.4.0 desktop-v1.4.0
   ```
   The `v1.4.0` tag starts a minimal relay, which dispatches **Release stable**
   on protected `main-v2`. Its preflight requires all three tags to exist on the
   exact current `main-v2` commit, renders the reviewed release notes, and runs
   the cache guard. It then **waits once for a configured reviewer to approve the
   GitHub `release` environment** before invoking all three publishers. The
   approval records the immutable release commit. Before any publisher starts,
   **Release stable** runs a zero-publication AMD64/ARM64 SignPath preflight for
   both the payload and installer stages. Every publisher checks out that SHA
   and fails if its remote tag moves afterward.
   Windows signing then runs automatically under SignPath trusted-build and
   origin verification: each architecture signs its installed executable
   payload first, rebuilds the portable archive and NSIS package, and finally
   signs the outer installer.
   A stable `npm-v*` publish moves the `latest` dist-tag automatically (build.mjs)
   and release-npm.yml verifies it landed. **Do not skip the npm tag**: the stable
   preflight fails when the matching `npm-v*` or `desktop-v*` tag is missing or
   points elsewhere. That guard exists because 1.0.0–1.17.5 shipped without
   stable npm tags and `npm update -g` silently downgraded users to 0.53.2 (#5822).
   The CLI and npm jobs run concurrently; the CLI's freshness check may warn while
   npm is still propagating, while release-npm.yml's verify step owns the final
   assertion. The stable orchestrator finishes with a postflight that verifies
   both GitHub Releases contain their required assets and npm `latest` exactly
   matches the approved version; missing artifacts can no longer produce a green
   stable run.
7. **Next cycle** — Preview rolls on toward `1.5.0`.

### Release-PR frequency rule

- Do not open a dedicated PR merely because a version is being published.
- Prefer the final same-repository product, fix, or release-infrastructure PR
  that already defines the candidate boundary. Generate the notes into that PR
  before its final review.
- A dedicated release-notes PR is the safe fallback, not the default. Use it
  when no suitable PR exists, the only candidate PR comes from a fork, or the
  release copy needs an independent approval boundary.
- Never bypass protected `main-v2` with a direct catalog commit. Reducing PR
  frequency must not remove release-copy review, catalog validation, cache
  checks, atomic tags, the single `release` environment approval, or public
  artifact postflight.
- Release infrastructure and release-note schema changes still require their
  own focused PR. This rule only avoids redundant version-only PRs.

## Notes

- Native CLI Preview supplies the protected tag's explicit `N`; the unified
  Preview orchestrator reuses that ordinal for Desktop `preview.N` and npm
  `canary.N`. The exact versions are recorded in one reviewed changelog entry;
  the immutable publication marker adds the actual candidate SHA after all
  surfaces succeed.
- A stable `-rc` tag (e.g. `npm-v1.4.0-rc.1`) still ships under `next`, not `canary`.
- Recover an interrupted stable release by dispatching **Release stable** from
  protected `main-v2` with the existing `vX.Y.Z` tag. Recovery requires the CLI,
  npm, and Desktop tags to remain aligned on an ancestor of current `main-v2`,
  then uses the same single approval and postflight. Never move or recreate the
  published tags to pick up a workflow fix.
- Recover an interrupted Preview release by rerunning **Release preview** from
  protected `main-v2` with the existing `vX.Y.Z-preview.N` tag. Do not dispatch
  the CLI, Desktop, or npm publisher directly: those paths cannot create the
  all-surface `release-event.json` marker and are blocked from public Preview
  publication. npm recovery verifies and reuses packages already published from
  the approved candidate, fills only missing packages, and never rolls `canary`
  back when a newer Preview is already public. Standalone Desktop Preview
  dispatch remains available only for non-publishing SignPath preflight and
  production-signing smoke checks.
- Windows release signing uses SignPath trusted-build, origin verification, and
  malware scanning. Keep the checked-in `windows-payload` and
  `windows-installer-v2` artifact configurations synchronized with the matching
  SignPath project slugs before merging a workflow that references them. Keep
  the legacy `windows-installer` and internal test configurations available for
  older release refs and signing validation, but never use them for public
  Preview or Stable artifacts.
  `windows-payload` signs the desktop, Guard, launcher, update helper, CLI, and
  generated NSIS uninstaller. `windows-installer-v2` verifies those trusted
  payload signatures before signing the rebuilt outer installer. The release
  certificate requires SignPath approval for every request. Preview and Stable
  reach Windows signing only after their GitHub environment approval; a
  dedicated SignPath CI identity then approves each payload and installer
  request through the SignPath API. Human SignPath approvers remain available
  for emergency recovery, but normal releases do not require additional
  SignPath clicks. SignPath must restrict `release-signing` to the trusted
  `.github/workflows/release-stable.yml`,
  `.github/workflows/release-preview.yml`, and
  `.github/workflows/release-desktop.yml` build definitions and exact `main-v2`
  branch. Stable and prerelease tag events are relayed to that protected control
  plane; do not replace exact matches with wildcards. The machine-readable
  `.signpath/contracts/release-signing.yml` policy is checked against the parsed
  workflow call graph in CI. Standalone Desktop releases require
  `SIGNPATH_RELEASE_SIGNING_ATTESTATION` to match the current contract hash;
  `signing_preflight=true` refreshes it only after both architectures complete
  both signing stages. Stable and unified Preview perform the same live
  preflight in their approved runs before CLI, npm, or Desktop publication, so
  a provider-side policy drift fails before any public channel starts.
- Desktop in-app updates use R2 first, then the `crash.reasonix.io` desktop release
  gateway. The gateway resolves the `desktop-v*` release line directly and never uses
  GitHub's repository-wide `/releases/latest`, because plain `v*` tags are the CLI
  release line. Stable CLI releases also carry a compatibility `latest.json` asset so
  older desktop builds that still use GitHub `latest` do not 404.
- Preview uses R2 plus the same gateway proxy for `preview/`; it never appears
  on the GitHub releases page. Releases also mirror the legacy `canary/`
  pointer, and the gateway accepts `/canary/`, until old desktop clients age out.
- DeepSeek is an editorial drafting dependency, not a runtime or publishing dependency.
  The API key is available only to the manually dispatched preparation workflow; tag
  workflows publish the reviewed JSON already committed to `main-v2` and never call a model.
- Windows applies the minisign-verified NSIS installer in place. Linux portable
  (`.tar.gz`) installs replace binaries in the install directory; Linux `.deb`
  installs download a signed package, authorize via Polkit, and upgrade with
  apt. The first `.deb` that ships the update helper is a one-time bootstrap
  (manual `sudo apt install ./Reasonix-linux-amd64.deb`). macOS applies in-app
  only for Developer ID signed and notarized builds; ad-hoc/local builds fall
  back to the download page. Desktop `latest.json` keeps `platforms` for
  portable channels and adds optional `native_packages` for OS packages.
