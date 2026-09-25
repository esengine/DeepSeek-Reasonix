# Releasing Reasonix

Reasonix prepares immutable, accepted release candidates before it creates any
public tag. Publication consumes those exact files after one approval; recovery
observes public state and fills only missing stages.

The public identity remains compatible with existing clients:

| Surface | Immutable tag | Public result |
| --- | --- | --- |
| CLI | `vX.Y.Z` | GitHub Release and an immutable R2 record |
| npm | `npm-vX.Y.Z` | none; the tag records identity only |
| Desktop | `desktop-vX.Y.Z` | signed GitHub Release, immutable R2 directory, and Stable manifest |

This line does not publish the CLI to npm, the Homebrew tap, or the R2 CLI
update pointers (`cli/stable/latest.json`, `cli/preview/latest.json`). Those
install channels belong to the Studio line, and no workflow here holds their
credentials.

All three tags must identify the candidate product SHA. They are created in one
atomic push and must never be moved, deleted, or recreated. The Go SDK module
(`sdk/go`) retains its independent `sdk/go/vX.Y.Z` version line.

## Normal release

1. Run **Prepare release** with `X.Y.Z` and review the generated bilingual
   Notes PR.
2. Merge the Notes PR after its required checks and review complete. The merge
   automatically starts **Prepare release candidate**. A maintainer may also
   dispatch that workflow on protected `main-v2` with `version`. Both entrypoints
   freeze the protected event SHA before the runner starts; arbitrary SHA inputs
   and PR-head execution are not accepted.
3. Wait for the candidate workflow to build the shared CLI/npm binaries, build
   and sign all Desktop platforms, run native final-package acceptance, and
   seal the payload and evidence. Record the candidate ID printed in its
   summary, for example `v1.39.0-0123456789ab-abcdef012345`.
4. From an authenticated maintainer checkout, run:

   ```sh
   ./scripts/release-stable.sh CANDIDATE_ID
   ```

5. Review the candidate ID, full product SHA, Notes digest, signing policy,
   platform receipts, and payload hashes in **Publish release candidate**.
   Approve its `release` environment once.
6. Wait for CLI and Desktop publication, Stable pointer convergence, the
   owned Pages deployment, hydrated download verification, and the publication
   ledger.

The candidate is fixed when Notes are reviewed. Later `main-v2` merges do not
invalidate it. A product fix or a change to embedded Notes creates a new
candidate. Do not replace source files during packaging or claim an old binary
contains new embedded Notes.

## Candidate contract

The record binds the candidate ID, version, full product SHA, build and
acceptance control SHAs, Notes catalog and rendered-body hashes, workflow run
and attempt, exact payload artifact ID, signing fingerprint, every file size
and SHA-256, and native Windows/macOS acceptance receipts. GitHub artifact
attestations bind both the record and every payload file to the protected
candidate workflow on `main-v2`.

Candidate payloads are retained for 30 days. Records, native receipts,
publication ledgers, and timing reports are retained for 90 days. An expired
unpublished payload must be prepared again. Published files are verified from
their immutable public channels and are not rebuilt because an Actions artifact
expired. Candidate artifacts contain no credentials or real user data.

To revoke an unpublished candidate, add its exact ID to the comma- or
whitespace-separated repository variable `RELEASE_REVOKED_CANDIDATES`.
Preparation reuse and publication both fail closed for listed IDs.

The six CLI binaries are each built once. CLI archives, the cask file, and npm
platform tarballs in the sealed payload reuse those bytes; only the archives
are published. Windows architectures build in parallel, then share one Certum
session; completed architecture bundles can be reused by a failed-job rerun. Windows native acceptance runs in parallel after
signing. Desktop platforms do not wait for unrelated platform acceptance before
starting their own downstream work.

## Publication and recovery

Publication verifies the record attestation, exact artifact IDs, payload
attestations and hashes, Notes identity, protected source ancestry, signatures,
and acceptance receipts before requesting approval. It then atomically creates
the three tags and publishes CLI and Desktop in parallel from the sealed
payload. Tag creation no longer starts a second legacy release pipeline.

For any interrupted publication, run:

```sh
./scripts/release-stable.sh CANDIDATE_ID recover
```

Recovery uses the same global publication lock and one approval.

If activation has not started, recovery creates all three absent tags in one
atomic push. If all tags already identify the candidate, it reuses them.
Partial tag sets and conflicting identities always stop recovery.

Each publisher re-reads its external state:

- matching immutable content is reused;
- missing content is uploaded;
- ambiguous requests are queried before retrying;
- conflicting immutable content stops the stage;
- signing and native acceptance are not repeated;
- a newer R2 or site pointer is never rolled back by an older candidate
  recovery;
- a site-only failure reruns Pages and hydrated-site verification without
  rebuilding product files.

The publication ledger records observed tag SHAs, every CLI and Desktop release
asset, and the Stable manifest, changelog, and homepage state. Recovery always
queries the actual service again; the ledger is evidence, not a source of truth
for later mutations.

## Verification and timing

Run **Verify release** with `X.Y.Z` for a read-only public check. It validates
the immutable tags, GitHub release contents, the current Stable manifest when
the version owns it, changelog, and the browser-hydrated download DOM. For an
older version, newer public pointers are preserved and reported rather than
treated as a reason to roll them back.

Candidate and publication workflows upload JSON timing evidence and summarize
queue, runner, build, signing, acceptance, upload, site deployment, and total
wall time. Diagnostic timing failures do not invalidate a sealed candidate or a
verified publication.

## Legacy recovery

**Legacy release recovery** remains available only for releases created before
the candidate pipeline. It retains historical surface selection and recovery
guards. New releases must use candidate IDs; do not add hard-coded run IDs or
version exceptions to the new workflows.

Historical Preview, Canary, and RC artifacts remain readable, but those paths
are not normal publication entrypoints.

The release is complete only when the immutable files, current public pointers,
hydrated website, publication ledger, and read-only verification all agree.
