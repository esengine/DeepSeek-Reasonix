# Releasing Reasonix

Reasonix has one user-facing release line: the official `X.Y.Z` version. The
release engine keeps the proven Stable publication topology: three immutable
Git tags on one `main-v2` commit and one protected orchestrator.

| Surface | Immutable tag | Public result |
| --- | --- | --- |
| CLI | `vX.Y.Z` | GitHub Release and Homebrew |
| npm | `npm-vX.Y.Z` | root and platform packages; `latest`, `canary`, and `next` compatibility aliases |
| Desktop | `desktop-vX.Y.Z` | signed GitHub Release, immutable R2 directory, and `latest/latest.json` |

The three tags are implementation identities, not user-selectable channels.
They must always resolve to the same commit and may never be moved or deleted.

The Go SDK module (`sdk/go`) is versioned independently of the three product
surfaces: its tags look like `sdk/go/vX.Y.Z` (first: `sdk/go/v1.0.0`) and
point at the release commit that first shipped the corresponding Extension
Protocol major. SDK tags do not trigger product releases, do not move, and
do not change the three-tag contract above.

## Daily release flow

The normal developer path has one version input, one reviewed Notes PR, one
terminal command, and one environment approval:

1. Open Actions → **Prepare release** and enter `X.Y.Z`.
2. Review and merge the generated bilingual release-notes PR.
3. From an authenticated maintainer checkout, run:

   ```sh
   ./scripts/release-stable.sh X.Y.Z
   ```

4. Approve the resulting **Release stable** run once in the `release`
   environment.
5. Wait for its postflight to verify CLI, npm, Desktop, R2, Homebrew, and the
   changelog.

If repository policy prevents Actions from opening the Notes PR, the workflow
still pushes `release-notes/vX.Y.Z` and prints this recoverable handoff:

```sh
gh pr create --repo esengine/DeepSeek-Reasonix \
  --base main-v2 --head release-notes/vX.Y.Z --fill
```

Do not rerun Notes generation merely because PR creation was denied.

## What the tag helper proves

`scripts/release-stable.sh` fails before creating any public ref unless:

- the version is canonical `MAJOR.MINOR.PATCH`;
- remote `main-v2` is the commit that introduces or updates the complete,
  reviewed Stable catalog record;
- exact-commit `main-v2` CI completed successfully;
- `vX.Y.Z`, `npm-vX.Y.Z`, and `desktop-vX.Y.Z` are all absent.

It then pushes a no-op guard for that exact `main-v2` SHA and all three
lightweight tags with one atomic Git transaction. If `main-v2` advanced while
CI was running, the complete transaction is rejected and no version tag is
consumed. A partial tag set is therefore not a normal failure mode. The
`vX.Y.Z` event starts the existing protected Stable relay; maintainers do not
dispatch child CLI, npm, or Desktop publishers.

## Publication and approval

The protected Stable workflow re-resolves all three tags to one SHA on
`main-v2` history, revalidates that normal candidates introduced their reviewed
Notes and passed exact-SHA push CI, and runs the cache guard before requesting
the sole human approval. `main-v2` may safely advance after the atomic tag
transaction without invalidating that candidate. After approval it performs a
no-publication SignPath preflight, then runs CLI, npm, and Desktop publishers
against the immutable candidate.

The npm publisher advances `latest`, `canary`, and `next` to the same official
version. `canary` and `next` remain only so historical scripts continue to
install a supported build; they are not testing channels and are not advertised.

No custom GitHub App, App private key, repository-owner setting change, manual
tag UI, or child-workflow approval is required.

## Recovery

For a partial Stable publication, open **Release stable** on protected
`main-v2`, enter the existing `vX.Y.Z`, select only the missing surfaces, and
approve `release` once. Recovery accepts only an immutable three-tag set that
remains on `main-v2` history. It must reuse matching public content and fail
closed on conflicting checksums, signatures, manifests, npm provenance, or R2
objects.

Never move, delete, or recreate a published tag. Ship product corrections as a
higher patch version.

## Retired prerelease paths

Normal Preview, Canary, and RC publication entrypoints are disabled. Historical
tags, Releases, package versions, changelog pages, and the final bridge endpoints
remain available for compatibility, but they do not appear in current download
navigation or release preparation.

Old CLI and Desktop channel settings resolve to the official line. Frozen
Preview endpoints continue to lead old clients to the bridge build, which can
then upgrade to the current official release.

## First release after cutover

For the first release after this change, independently prove:

- the three tags resolve to the reviewed Notes merge SHA;
- both GitHub Releases contain their complete expected assets;
- npm root and all six platform packages report that SHA and
  `latest == canary == next`;
- R2 immutable and latest manifests are byte-identical and every URL works;
- Homebrew and reasonix.io show the same version;
- old bridge clients can upgrade to the official release.

The release is incomplete until every public surface reaches a terminal,
verified state.
