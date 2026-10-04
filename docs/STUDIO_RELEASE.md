---
owner: @esengine
backup: @SivanCola
status: active
reviewed: 2026-09-30
---

# Studio release runbook

Workflow: `.github/workflows/release-studio.yml`. Trigger: push of a `studio-vX.Y.Z` tag.

## 1. Owners

| Role | Person | Responsibility |
| --- | --- | --- |
| Release owner | @esengine | Decides the version, writes the notes, pushes the tag, verifies, recovers. |
| Backup | @SivanCola | Runs this runbook when the release owner is unavailable. |

## 2. Preconditions

| ID | Check | Command |
| --- | --- | --- |
| P1 | The release commit is the fetched head of `origin/studio`. | `git fetch origin studio && git rev-parse origin/studio` |
| P2 | Every job of `CI` and `Studio` succeeded on that commit. A flaky job is rerun, not ignored. | `gh run list --branch studio --commit <sha> --json name,conclusion` |
| P3 | `release-notes/studio/X.Y.Z.md` is in that commit and passes `make check`. | `git cat-file -e <sha>:release-notes/studio/X.Y.Z.md` |
| P4 | The version is valid semver and above the latest tag. | `git tag -l 'studio-v*' --sort=-v:refname \| head -1` |
| P5 | The Windows signing mode is known. `true` signs with Certum and blocks the release if signing fails; anything else ships unsigned and the body says so. | `gh variable list \| grep STUDIO_SIGNING` |

## 3. Steps

1. Write `release-notes/studio/X.Y.Z.md` (format in section 6), commit, push to `studio`.
2. Wait until P2 holds for the commit that contains the notes.
3. Tag the verified commit and push the tag:

   ```bash
   git fetch origin studio
   SHA=$(git rev-parse origin/studio)
   git tag studio-vX.Y.Z "$SHA"
   git fetch origin studio && test "$(git rev-parse origin/studio)" = "$SHA"
   git push origin studio-vX.Y.Z
   ```

4. Watch the run until it finishes:

   ```bash
   RUN=$(gh run list --workflow release-studio.yml --limit 1 --json databaseId --jq '.[0].databaseId')
   gh run watch "$RUN" --exit-status
   ```

Jobs in the run:

| Job | Does | Gate |
| --- | --- | --- |
| `resolve` | Validates the tag shape and that the commit is on `studio`. | fails on any other ref |
| `signing-contract` | Validates `.signpath/contracts/release-signing.yml` against the workflows that reach the Certum credentials and prints its fingerprint. | fails on an undeclared signing workflow |
| `build` | Builds windows/amd64, darwin/amd64, darwin/arm64, linux/amd64; signs macOS. With signing on, the Windows leg uploads its bundle instead of packaging it. | Apple secrets are required |
| `windows-sign-payload` | Only with `STUDIO_SIGNING_ENABLED=true`. Refuses a bundle whose PE files differ from the declared list, signs the release PE files, verifies them and the two Microsoft-signed DLLs, and records a digest of the whole signed tree as a job output. Installs no toolchain. | shared concurrency group `certum-signing`, environment `studio-release` |
| `windows-package` | Builds the installer and zip from the signed tree as it came. Holds no secrets. | none |
| `windows-verify-package` | Checks the payload against the recorded digest, unpacks the zip and the installer, requires both trees to match the payload file for file, and outputs the SHA-256 of both packages. Holds no secrets and no environment. | none |
| `windows-sign-installer` | Requires both packages to hash to the checked values, signs the installer, verifies its signature, and outputs the signed installer's SHA-256. Opens no archive. | shared concurrency group `certum-signing`, environment `studio-release` |
| `cli` | Builds `reasonix` archives for six OS/arch targets plus `SHA256SUMS`. | fails on a missing archive |
| `publish` | Renders the notes with their authors, minisigns, writes `latest.json`, creates the GitHub prerelease, mirrors to R2. | environment `studio-release`; skipped unless all four Windows signing jobs succeeded or signing is off; an unresolved `#N` stops it before signing |
| `cli-gate` | Only with `STUDIO_PUBLISHES_CLI=true`. Requires `CLI_PUBLISH_FROZEN=true`. Checks out nothing and reads no secret. | fails while 1.x is not frozen, and then no other CLI job runs |
| `cli-tag` | Only with `STUDIO_PUBLISHES_CLI=true`. For a stable or `-preview.N` version, creates the tag `vX.Y.Z` on the studio commit with the release tag identity (`RELEASE_TAG_TOKEN`, see below); an existing tag on that commit is kept, one elsewhere fails. | environment `studio-release`; runs after `publish` and `cli-gate`; checks out nothing, one inline step reads the token |
| `cli-channels` | Only with `STUDIO_PUBLISHES_CLI=true` (unset today; 1.x owns the channels). Publishes `reasonix` and `@reasonix/cli-*` to npm with `--provenance`, then updates the Homebrew cask (not for a candidate). | no environment and no approval; `id-token: write` on this job only; runs after `publish`, `cli-gate` and `cli-tag` |
| `cli-pointer` | Only with `STUDIO_PUBLISHES_CLI=true`. Creates the GitHub release `vX.Y.Z` holding the CLI archives, writes `cli/releases/vX.Y.Z/latest.json`, then moves `cli/stable/latest.json` (a `-preview.N` version moves `cli/preview/latest.json`; any other prerelease publishes nothing). | runs after `publish`, `cli-gate` and `cli-tag`; serialized per channel by the `studio-cli-pointer-<channel>` lock |

The `studio-release` environment allows the `studio-v*` tag and the `studio` branch. It has no required reviewer.

Publishing to the CLI channels needs no human approval. Three controls stand in for it:

- The variable `STUDIO_PUBLISHES_CLI` is off by default, and the job is skipped unless it is `true`.
- The tag protection rule `Protect release tags` covers `studio-v*`, so only someone with write access can push the tag that starts a release.
- `npm publish --provenance` attaches a Sigstore attestation naming this repository and commit.

Recovery of the CLI pointer:

- `cli-pointer` trusts an existing release `vX.Y.Z` after checking it on its own terms: tag, asset names, sizes, URLs, and `SHA256SUMS` against the release's own digests.
- It never compares the release with the rerun's archives, which are not byte-reproducible.
- If the release was created and the R2 write failed, a `workflow_dispatch` run finishes the job.
- The pointer script refuses to run unless `CLI_PUBLISH_FROZEN=true`, and re-reads the pointer after writing it.

The tag identity:

- Only a person may create `v*` tags (release ruleset), so `cli-tag` uses the secret `RELEASE_TAG_TOKEN` and variable `RELEASE_TAG_ACTOR` that the 1.x release workflow also uses. This workflow is a second user.
- One inline step reads the token, in a job that checks out and runs no repository code. It creates only the tag, through the refs API; `cli-pointer` creates the release with the run's own token.
- It refuses unless the repository is the official one, the ref is a `studio-v*` tag or `studio`, `CLI_PUBLISH_FROZEN` is true, and the credential belongs to `RELEASE_TAG_ACTOR` with push access.
- It also refuses unless the studio tag still resolves to the approved SHA and that SHA is on `studio` history.
- An existing tag on the same commit (lightweight or annotated) counts as done; on another commit it fails.
- The `studio-release` environment restricts where the job runs. It does not protect the secret: `RELEASE_TAG_TOKEN` is repository-level, so anyone who can push a workflow can read it. The owner is the only writer today.
- The 1.x identity script is not reused: it pins `main-v2` and would need a checkout. The server enforces the rulesets at the tag write.

Once a GitHub release `vX.Y.Z` exists, the v1.39.5 client's GitHub-list fallback and the gateway's CLI fallback select it, so turning the switch on is the switch of the update channel itself.

While the variable is `true`, pushing a `studio-v*` tag publishes to npm `latest` and to Homebrew. The `workflow_dispatch` recovery run is subject to the same switch.

The job reads the repository secrets `NPM_TOKEN` and `HOMEBREW_TAP_TOKEN`, the same pair the 1.x line uses. On the day of the switch, freeze 1.x in the same step by setting its variable `CLI_PUBLISH_FROZEN`, so the two lines never write the channels at once.

No other job requests `id-token` or references these two secrets, and no workflow file reads them but `release-studio.yml`'s `cli-channels`; `cmd/signpath-contract` tests this over the parsed YAML of every workflow, and that `cli-channels` declares no environment.

Verify a published package with `npm view reasonix dist.attestations`: the attestation is present and names this repository.

## 4. Verification

| ID | Expected | Command |
| --- | --- | --- |
| V1 | Prerelease exists with 22 assets (per-platform packages, `.minisig` files, CLI archives, `latest.json`, `SHA256SUMS`). | `gh release view studio-vX.Y.Z --json isPrerelease,assets --jq '.isPrerelease, (.assets \| length)'` |
| V2 | The catalog lists the new version first. | `curl -s https://dl.reasonix.io/studio/versions.json \| jq -r '.versions[0].tag'` |
| V3 | The manifest is served. | `curl -sI https://dl.reasonix.io/studio-vX.Y.Z/latest.json \| head -1` |
| V4 | The body contains the version notes and the standing install text. | `gh release view studio-vX.Y.Z --json body --jq .body` |

## 5. Recovery

| Symptom | Cause | Action |
| --- | --- | --- |
| No run appears, or a rerun ends in `startup_failure` with no jobs. | GitHub Actions runner outage. | Wait for queued runs to drain, then `gh workflow run release-studio.yml --ref studio -f tag=studio-vX.Y.Z`. |
| A build or publish step failed. | Workflow or runner fault. | Fix on `studio` if needed, then dispatch as above. The dispatch rebuilds from the tag's commit with the workflow from `studio`. |
| A signing job fails with an error titled `studio-signing.*`. | A credential or an expected-signer variable is missing or malformed; the title names which. | Fix it (section 7), then dispatch the same tag. |
| A signing job fails at `Connect to Certum` or `Sign the executables`. | SimplySign login, OTP or certificate problem. | Run the smoke test (section 7). To ship unsigned instead, set `STUDIO_SIGNING_ENABLED=false` and dispatch. |
| A signing job fails with `Unexpected signer subject` or `thumbprint`. | The certificate changed, or a value was copied wrong. | Compare with the smoke test's summary; correct `STUDIO_SIGNING_SUBJECT` or `CERTUM_KEY_ID`. |
| A signing job waits before starting. | A Studio or 1.x smoke test or release holds `certum-signing`. | Wait. A second run queued behind the same group cancels the earlier queued one; dispatch again if that happens. |
| Signing is restored after an unsigned release. | Artifacts were published unsigned. | Set `STUDIO_SIGNING_ENABLED=true` and dispatch the same tag; `publish` replaces the assets. |
| The body is missing or wrong. | Notes are read from the tag's commit, not the branch. | `gh release edit studio-vX.Y.Z --notes-file <file>`; append the standing text from the previous body. |
| The tag points at the wrong commit and `publish` has not run. | Tagging error. | `git push origin :refs/tags/studio-vX.Y.Z`, delete the local tag, restart at step 3. |
| The tag points at the wrong commit and `publish` has run. | Tagging error after release. | Do not move the tag. Release the next patch version. |

## 6. Release note format

File: `release-notes/studio/X.Y.Z.md`, without the tag's `v`. Language: Chinese. Enforced by `release-note` and `doc-prose`.

| ID | Rule |
| --- | --- |
| R1 | The first line is a one-sentence summary. No other paragraph. |
| R2 | Group headings are `## 新增`, `## 变更`, `## 修复`, `## 移除`, `## 升级须知`, in that order, omitting empty ones. No deeper headings. |
| R3 | One change is one list item within 200 display columns. |
| R4 | Every item names its issue (`#123`), pull request or commit. |
| R5 | Describe what the user observes. The explanation belongs in the linked commit. |

The published body is rendered, not copied:

| Reference | Rendered as |
| --- | --- |
| `#N`, a pull request | `#N by @author` |
| `#N`, an issue closed by merged pull requests | `#N fixed in #M by @author` |
| `#N`, an issue fixed by a direct push; a commit; a bot author | as written |
| `#N`, a discussion or no such number | as written, with a warning |

The repository owner and any login in `RELEASE_CREDIT_EXCLUDE` (comma separated) are treated like bots: never named in an item or in the list.

A `## 贡献者` list of the credited authors closes the notes. A `#N` counts only at a line start or after whitespace, `(`, `（`, `、`, `，` or `,`, and never in code, an HTML comment or a link target. Write colours such as `#333` in a code span.

A lookup GitHub refuses, or cannot answer after retries, fails `publish` with a `release_credits.*` code. Preview with `node scripts/studio-release-notes.mjs release-notes/studio/X.Y.Z.md /tmp/notes.md`; it reads `GH_TOKEN`, else `gh auth token`.

```markdown
本版修复读图模型误报看不到图，并让被 ACL 残留阻塞的 Windows 安装恢复启动。

## 修复

- 读图模型不再声称看不到已附加的图片 (44150b0aa)
- Windows 安装目录带 AppContainer 包 SID 授权时窗口可以正常打开 #10435
```

## 7. Windows signing

Windows builds are signed through Certum SimplySign cloud signing with the project's certificate, the same account and certificate 1.x releases use.

| Rule | Detail |
| --- | --- |
| Signing authority | The private key stays in Certum's cloud, but whoever holds the three secrets can sign. |
| Default | Off until `STUDIO_SIGNING_ENABLED` is `true`; until then releases ship unsigned and the body says so. |
| Fail closed | With the switch on, a missing value or a failed signature blocks `publish`. |

| Name | Kind | Content |
| --- | --- | --- |
| `CERTUM_USERNAME` | repository secret | SimplySign account shared with 1.x |
| `CERTUM_OTP_URI` | repository secret | the complete `otpauth://totp/...` provisioning URI shared with 1.x |
| `CERTUM_KEY_ID` | repository secret | the certificate's 40-character SHA-1 thumbprint shared with 1.x |
| `STUDIO_SIGNING_SUBJECT` | variable | the certificate subject every signed executable must carry, exactly as the smoke test reports it |
| `STUDIO_SIGNING_ENABLED` | variable | `true` turns signing on |

Prerequisites. The environment alone does not confine the secrets: its deployment rule admits any `studio-v*` tag, and without a ruleset anyone with write access can push one, or edit a workflow on a branch the rule admits. Before enabling, the maintainer decides on:

| Setting | Effect |
| --- | --- |
| A tag ruleset on `studio-v*` restricting creation, update and deletion to maintainers | Only maintainers can start a signing release. |
| A required reviewer on `studio-release` | Every job that reads the secrets waits for a person; a release then asks for approval at each signing job and at `publish`. |
| `studio` branch protection requiring review | The workflow and scripts that do the signing change only through review. |

To enable signing:

1. Confirm the three repository secrets hold the project certificate; both lines read them, so replacing one changes 1.x too:

   ```bash
   gh secret set CERTUM_USERNAME
   gh secret set CERTUM_OTP_URI
   gh secret set CERTUM_KEY_ID
   ```

2. Run the smoke test. It signs two probes, publishes nothing, and writes the signer's subject, issuer, thumbprint and timestamp to the job summary:

   ```bash
   gh workflow run studio-certum-signing-smoke.yml --ref studio
   ```

3. Copy the subject from that summary, character for character: `gh variable set STUDIO_SIGNING_SUBJECT --body '<subject>'`.
4. Run the smoke test again. It now fails unless the signer's subject matches.
5. `gh variable set STUDIO_SIGNING_ENABLED --body true`. From the next release on, the four Windows signing jobs must succeed before `publish` runs, and the body states that Windows is signed. A missing secret or subject fails the release with an error titled `studio-signing.*`.

What is signed:

| File | Signed by | Why |
| --- | --- | --- |
| The release PE files | the project | Windows loads each one, and Smart App Control judges an unsigned DLL on its own reputation. |
| `d3dcompiler_47.dll`, `dxil.dll` | Microsoft, left as shipped | Verified to carry a trusted, timestamped signature from an `O=Microsoft Corporation` signer under a Microsoft PCA, chaining to Microsoft Root Certificate Authority 2010 by thumbprint. |
| The installer | the project | Signed only after `windows-verify-package` has matched its contents. |
| `resources/elevate.exe` | nobody | `windows-package` writes it after the payload is signed; its SHA-256 is pinned instead. |
| The NSIS uninstaller | nobody | electron-builder signs it only through an in-process hook, which would put the session in the packaging job. |

The PE files are declared in `scripts/windows-signing-lib.ps1`. A PE file is a `.exe`, `.dll` or `.node` file, or any file with an `MZ` header.

A bundle holding a PE file the list does not name fails with `studio-signing.undeclared-pe`, and one missing a listed file with `studio-signing.missing-pe`. A person adds it to the right list.

The release PE files as of 2.20.3:

- `Reasonix Studio.exe`, `resources/bin/reasonix-studio-host.exe`, `resources/bin/reasonix-computer-helper.exe`
- `dxcompiler.dll`, `ffmpeg.dll`, `vk_swiftshader.dll`, `vulkan-1.dll`

| Rule | Detail |
| --- | --- |
| Embedded signature only | Verification reads each file's embedded signature through SignTool. `Get-AuthenticodeSignature` answers from the Windows catalog for `d3dcompiler_47.dll`. |
| `elevate.exe` pin | The hash belongs to the electron-builder in the lockfile. An upgrade that changes it fails `windows-verify-package`; review the new file and update the pin. |
| One architecture | The installer must carry exactly one application archive, `app-64.7z`. |
| Installer code | The NSIS code comes from `windows-package`. Its application tree is checked; its installer logic is not. |

How the payload is carried from signing to publishing:

| Check | Where | Refuses |
| --- | --- | --- |
| Digest of every file in the signed tree, recorded as a job output of `windows-sign-payload` | `windows-verify-package` | a payload artifact replaced after signing |
| The zip's tree equals the payload, file for file | `windows-verify-package` | a file added, dropped or changed while packaging, PE or not |
| The installer's `app-64.7z`, unpacked with the runner image's 7-Zip, equals the payload plus the pinned `resources/elevate.exe` | `windows-verify-package` | the same, inside the installer |
| Both packages hash to the values `windows-verify-package` output | `windows-sign-installer`, before connecting | a package replaced after it was checked |
| The downloaded installer hashes to `windows-sign-installer`'s output and the zip to `windows-verify-package`'s, and nothing else is in the artifact | `publish` | a Windows artifact replaced after signing; fails with `studio-signing.package-hash-mismatch`, `package-hash-missing` or `package-set-mismatch` |

The SimplySign session can sign for any process on its runner while it is up. The two signing jobs therefore check out only the workflow's own commit, install no toolchain, parse no archive, and stop SimplySign after signing.

`windows-package` runs electron-builder and `windows-verify-package` unpacks its output, each on a separate runner with no secrets.

Studio and 1.x signing jobs and smoke tests share the concurrency group `certum-signing`: they run one at a time, so no two runs hold the shared SimplySign session at once.

## 8. Reference

| Name | Kind | Used by |
| --- | --- | --- |
| `APPLE_CERT_P12`, `APPLE_CERT_PASSWORD`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID`, `APPLE_API_ISSUER_ID` | secret | macOS signing and notarization |
| `CERTUM_USERNAME`, `CERTUM_OTP_URI`, `CERTUM_KEY_ID` | repository secret | Windows Authenticode signing shared with 1.x (section 7) |
| `STUDIO_SIGNING_ENABLED` | variable | Windows signing switch |
| `STUDIO_PUBLISHES_CLI` | variable | hands the CLI's npm and Homebrew channels to this workflow; set it only in the same step that freezes 1.x with `CLI_PUBLISH_FROZEN` |
| `NPM_TOKEN`, `HOMEBREW_TAP_TOKEN` | repository secret | `cli-channels` here; shared with 1.x |
| `STUDIO_SIGNING_SUBJECT` | variable | the signer subject every signed executable must carry; required when signing is on |
| `MINISIGN_PRIVATE_KEY`, `MINISIGN_PASSWORD` | secret | detached signatures verified by the updater |
| `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_ACCOUNT_ID`, `R2_BUCKET` | secret | artifact mirror and catalog |

| R2 path | Owner | Content |
| --- | --- | --- |
| `studio/versions.json` | this workflow | Studio catalog, newest first |
| `studio-vX.Y.Z/` | this workflow | artifacts, signatures, `latest.json` |
| `cli/stable/latest.json`, `cli/preview/latest.json` | `cli-pointer` | what `reasonix upgrade` reads through `crash.reasonix.io/v1/cli/releases/<channel>/latest.json`; only ever moves to a newer version |
| `cli/releases/vX.Y.Z/latest.json` | `cli-pointer` | immutable record of one CLI release; a rerun with different content fails |
| `versions.json` | desktop line | never written by this workflow |
