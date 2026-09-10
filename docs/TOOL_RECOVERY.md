# Durable tool recovery

Tool execution evidence belongs to the Go runtime. Electron and Remote tabs
share the same controller API; no Wails bindings or secondary recovery database
are used.

## Execution and persistence

1. Tool proposals retain their original arguments in the session.
2. Argument validation, target resolution and permission checks precede start.
3. The runtime assigns an attempt ID, canonical argument digest and stable
   idempotency key. The action receipt is saved through the existing session
   checkpoint path before `tool_started` is acknowledged and fsynced.
4. The actual tool executes with the idempotency key in its context.
5. A received result updates the same attempt before the result checkpoint.
   An explicit failure is distinct from cancellation or timeout after start.
6. On restart, started calls without confirmed results remain unresolved.
   New-format dispatched calls without a start barrier are cancelled. Legacy
   missing start evidence remains unknown, never proof that execution was absent.

The first writer also binds its action identity and transcript digest to the
existing file checkpoint. File postconditions establish current state, not the
historical outcome of an external operation.

Unresolved mutations block subsequent mutations even when a later model call
uses a new call ID. Read-only diagnosis remains available. Rewriting or compacting
the same session preserves unresolved effect receipts. A user-confirmed exact
tool/argument pair is not automatically executed again; confirmation is recorded
as `user_confirmed`, never as a fabricated successful tool output.

## Common transport contract

The desktop host exposes:

- `GetToolRecoveryForTab(tabID)`
- `ResolveToolRecoveryForTab(tabID, request)`

Both dispatch local and remote operations. Remote operations use authenticated
`GET /tool-recovery` and `POST /tool-recovery` on Serve. The POST also uses the
existing expected-session header and foreground ownership guard.

A snapshot includes `sessionPath`, `runtimeEpoch`, a content-derived `revision`,
`calls`, and `retryEnabled`. An action supplies the exact snapshot identity,
`attemptId`, `inspectionId`, and one of `inspect`, `confirm`, `reject`, or `retry`.
Controller admission excludes running, finishing, rotating and closed runtimes.
Snapshot, inspection and attempt mismatches reject without execution.

Normal snapshots omit original arguments. Explicit inspection returns the
stored parameters for display. They never enter the model recovery prompt.
Inspection can report `present`, `postcondition_satisfied`, `absent_fenced`, or
`unknown`. User confirmation requires inspection of that exact attempt. Rejecting
a retry leaves uncertainty recorded and does not grant permission for more writes.
After resolution, Continue uses ordinary turn submission and its existing fences.

## Safe retry boundary

Retry is disabled by default. Set `REASONIX_TOOL_RECOVERY_RETRY=1` on the owning
Go host to enable the UI action. Remote hosts decide their own capability.

An explicit retry gets a fresh call and attempt ID while keeping the original
idempotency key. It uses the ordinary validation, policy, permission, hook and
lease pipeline. Stored arguments are authoritative; the UI cannot substitute them.
The original receipt records its replacement attempt. Reusing its old action
request cannot run it again.

Read-only retries wait until prior stragglers are gone. A mutating tool must
implement `tool.EffectVerifier` and re-establish *absent and fenced* immediately
before retry. Its `RecoveryScope` binds the sink/account/resource and must still
match the recorded scope. Fenced means the previous attempt can no longer commit. A mere
absence observation is insufficient. Tools can consume
`tool.RecoveryIdempotencyKey(ctx)` to deduplicate at their own sink.

File tools reuse their existing `WriteVerifier` for read-only postcondition
checks. Generic shell commands and arbitrary MCP services cannot prove external
absence; they remain unknown. This implementation does not promise exactly-once
effects for external services lacking authoritative receipts or deduplication.

## Transcript and compatibility

`ValidateTranscript` checks an already normalized view without changing it.
The sampling gate runs after provider-request interceptors and before streaming,
using the same pairing normalization as the adapters. Proven host-rejected calls
may use valid empty arguments in the outbound repair view while preserving the
original local arguments and validation error. Healthy request bytes are unchanged.

Recovery receipts are additive local metadata. Existing JSON remains readable,
old event numbers remain stable, and old remote clients still pass through the
new server's execution fence. Old executable versions do not implement these new
recovery guarantees: finish recovery before downgrading the owning runtime.
No storage-format downgrade or migration rewrite is performed automatically.

Snapshots expose counts of unknown, confirmed, retried, rejected and blocked
actions derived from retained session evidence. They are not lifetime telemetry.

## Verification and rollout

- Core tests cover argument/permission rejection, failed durability, explicit
  outcome classification, immutable snapshots and stale attempt rejection.
- A child process exits after an fsynced external effect but before returning a
  result. Reload and two concurrent confirmations prove persistence and at-most-one
  resolution; the effect file contains exactly one write.
- Fake authoritative sinks exercise unknown, unfenced absence, fenced absence,
  stable idempotency keys and refusal of repeated retry requests.
- Chromium acceptance: `cd desktop/frontend && node bench/tool-recovery.mjs`.
  It checks inspection, confirmation, continuation, unsafe retry disabling, and
  a delayed inspection arriving after a session switch.

Deploy the Go runtime and generated Electron contract together. Start with retry
disabled, inspect unknown outcomes and confirmation receipts, then enable only
where tool-level sink contracts have been verified. Disabling the retry switch
preserves the journal and manual recovery path. Publishing and production rollout
are separate from local implementation and tests.
