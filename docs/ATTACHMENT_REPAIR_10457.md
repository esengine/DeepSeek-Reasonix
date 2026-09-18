# Attachment admission and request isolation (#10457)

[中文](ATTACHMENT_REPAIR_10457.zh-CN.md)

The attachment source is PR head
`b582ebabe089fb75a6ee8ba23dddcb0a48a873ca`. It was semantically replayed on
`main-v2` `93fb72870758f2ff60ae24386406b0e5f0782f0e`, which already contains the
durable-draft work from #10469 and the fixed-snapshot export implementation
from #10471, then rebased onto `main-v2`
`9b0cc56e5c0e60d92cb447b360ef8fa018c7c72b`. That base also includes #10500's
model-scoped reasoning capability resolution. `git range-diff` against the prior
`bd0733b9` replay changes only the generated Desktop contract digest, so the
attachment patch and later mainline behavior are both preserved. No changes
from #10346 are incorporated. The design borrows admission,
capability ownership and independent cancellation from deepseek-harness
`ddefc45fbc7f8e46dd73185e68295696d1297887`, without importing its image
normalization, Sharp dependency or global attachment store.

## Behavior and ownership

An explicit image is part of the submission, not a suggestion for a tool to
search the filesystem. Preparation validates the complete ordered batch and
persists originals before admission. Failure leaves the draft intact and does
not start the provider, tools, a user message or Goal setup. Content objects
published before a later failure are harmless unreferenced objects; rollback
never removes originals that another submission may already reference.

`PreparedSubmission` is a host-only candidate. The final admission gate checks
its controller and storage scope again and looks up the receipt before source
access. A versioned fingerprint describes text, action and logical attachment
identities; it excludes temporary credentials and accepted content digests.
The acceptance event contains the frozen image references. Deferred turn
bodies capture those references before parking and never read temporary
submission state asynchronously. Legacy gate-owned synchronous adapters remain.

The target RPC captures tab generation, controller, workspace, host/session
storage identity and runtime epoch. Capture precedes file reads and hashing.
Changing focus does not retarget work; retiring the target invalidates it.
The controller service owns draft credentials; the host shares only immutable
content and discardable variants. Same-session runtime replacement transfers
credential proofs. Validation on an explicit user retry renews credentials;
repeated renewal returns the same credential. Releasing either proof revokes
that draft family. Rebinding never sends a message. Successful Composer cleanup
is conditional on the exact submitted draft version, including queue receipts.

Late session publication also binds the attachment store to that runtime's
authoritative content graph. Creating a controller before its session must not
pin attachments to a legacy fallback directory. `attachment_owner_test.go`
reproduces that construction order and verifies subsequent runtime renewal.

## Finding-to-fix map

| Finding | Shared repair | Regression evidence |
| --- | --- | --- |
| Accepted images absent from actual requests | Carry `ImageInputs` through ordinary, edited, Goal, invocation and queued turns; resolve before protocol normalization | `attachment_endpoints_test.go`, `attachment_routing_test.go`, loopback provider capture |
| Legacy workspace path escape | Open under `os.Root`, refuse leaf links, compare opened/path identities before and after reading; nonblocking Unix open | `isolation_regression_test.go`, workspace/permission identity tests |
| Export omits images or damages an existing target | Extend #10471 `ExportSnapshot` with the authorized ordered image closure and task-private rendering; retain its no-replace publication and recovery journal | `sessionexport/build_test.go`, session attachment closure and #10471 publication tests |
| Queue depends on temporary draft | Persist ordered inputs and source aliases; validate immutable objects before claim; prepare edits before replacing old envelopes | Queue endpoint, corruption-block and mixed-source edit tests |
| Retry creates a second turn or rereads deleted sources | Lookup durable versioned receipt before preparation and again under admission gate | Draft-release retry and canonical credential fingerprint tests |
| Mixed attachments bypass batch limits | Merge structured, legacy and authorized sources; validate all originals with one policy | Invalid endpoint batches, mixed-source/dedup, 20/21 and byte-boundary tests |
| Service/draft lifetime follows focus or runtime accidentally | Owner-created service, lifecycle-bound contexts, opaque target tokens and validated credential renewal | Desktop target, replacement, cancellation and Composer tests |
| Canceled transform captures a later request | Remove last-waiter inflight entry under lock; only its own task may finish/remove it | Deterministic independent-waiter and successor tests with race detector |
| Encoding/upload follows wrong model/account | Capture provider configuration; resolve by actual parent/child/understanding model; propagate cancellation | Upload route interception, real boot child and text-to-vision provider tests |

Test paths are under `internal/control`, `internal/attachment`, `internal/boot`,
`internal/session`, and `desktop`. They assert actual provider messages in
addition to preparer return values. Tool originals are persisted before an
understanding request. A persistence failure preserves completed tool text and
diagnostics rather than replaying an action that may have side effects.

## Compatibility and cache contract

- Originals, `.content-v1`, revision 3 and inbox schema 3 stay in place.
- Legacy `Images []string`, system prompts, tool schemas and history order are
  not rewritten. Three loopback protocol tests compare the actual serialized
  OpenAI, Responses and Anthropic bodies before/after legacy resolution.
- New images use the existing deterministic policy v1 (1568-pixel bound,
  JPEG quality 85 where applicable); the 512 MiB variant cache never deletes
  original objects. Both source validation and encoding use the same host
  heavy-work limit of two.
- New image request bytes differ from the broken request that omitted images;
  the first prompt-cache miss for those submissions is expected.
- Unknown receipt versions and unverifiable legacy receipt mismatches fail
  explicitly instead of guessing and replaying.
- `attachments-v2` target operations are negotiated explicitly; unsupported remote hosts do not
  silently fall back to another workspace.
- Schema compatibility tests prove that unsupported revision 3 data is rejected
  without an in-place downgrade. Packaged previous-binary validation remains a
  separate release-qualification step and is not inferred from source tests.

## Reproducible qualification

Qualification has three risk-based layers.

The PR gate runs after code changes and blocks a push on failure. It covers the
focused attachment, submission, queue, history-preview and variant-cache Go
tests; deterministic concurrency tests and relevant race suites; Desktop and
Composer attachment tests; type checking, contract freshness, `repolint`,
inventory, the preview bundle budget, actual provider image digests, zero
provider/tool/Bash calls after explicit-image rejection, and unchanged legacy
`Images []string` bytes for OpenAI, Responses and Anthropic. A substantial shared
code change also runs the root, Desktop and frontend suites once. Later changes
limited to documentation, mocks or package splitting rerun their affected suites
and static gates.

The patch-id-equivalent integration candidate completed the root and Desktop Go
suites, focused race suites, all 409 frontend suites, frontend type checking,
the production build and bundle budgets, generated-contract freshness, inventory
and repository lint. The final rebase preserves the attachment patch according
to `range-diff` and reruns the affected model-routing, Desktop and frontend gates.
Desktop Linux cross-lint still reports four unchanged tray stubs from the
`main-v2` baseline; it is not an attachment regression.

The final-SHA package gate runs once before pushing. Build with
`scripts/desktop-build.sh`, extract the **finished** ZIP into a new directory,
and run:

```sh
node desktop/packaging/smoke.mjs /isolated/Reasonix.app
node desktop/packaging/attachment-native-smoke.mjs \
  /isolated/Reasonix.app /isolated/evidence
```

The attachment fixture launches the production Electron shell and bundled
service with a disposable data home and a loopback provider. It checks exact
image bytes with a process directory distinct from the workspace under
workspace-write permission, `view_image` parity, cancellation/retry without a
duplicate turn, and real Composer preservation after an image read fails. It
writes JSON evidence and a screenshot without installing or replacing an app.

The source branch previously produced an isolated preview package from
`86fd76f20d7d` and loopback evidence for its attachment flow. That evidence is
historical input, not qualification of the integrated head. Generated package
evidence remains uncommitted; a release candidate must rerun the ordinary package
smoke and `attachment-native-smoke.mjs` against its own SHA before making a
packaged-runtime claim.

Release and high-risk gates are conditional. Storage revision or inbox schema
changes require previous-version upgrade/downgrade checks; shell, signing or
service-layout changes require native signing and startup matrices; permission
model changes require workspace-write/full-access comparison; provider, Files
API or upload-cache changes require live-provider and account-isolation checks.
This PR keeps revision 3 and inbox schema 3 and does not change Files API or
upload caching, so previous-binary and live-network checks are not applicable.

Loopback capture proves image transport, routing and permission behavior, not
semantic recognition by a paid remote model. No live credential is required or
read by these fixtures. Crash publication recovery runs when the same destination
is reopened for publication; there is no scan of arbitrary export directories at
application startup.

The repair does not merge or release the PR, overwrite an installed application,
change Bash search behavior, add upload caching or collect original objects.
