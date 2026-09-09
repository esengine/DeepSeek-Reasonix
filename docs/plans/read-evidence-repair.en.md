# Read evidence repair and verification

This completes the execution wiring of the existing single-PR read redesign.
The coordinator owns continuation and final-answer decisions in the default
pipeline; the legacy incomplete-read machine is used only for rollback.

The actual reader produces output and source identity from the same captured
bytes or overlay buffer. Disk inspect/range calls capture identity only up to 256 KiB;
larger local reads remain bounded streaming reads. Explicit full reads and
their host continuations capture at most 64 MiB. Unversioned windows cannot be stitched into a full-file
claim; an incomplete full requirement becomes `needs_scope`. These are internal
resource bounds, not new user settings. Read tokens are clipped to live context
headroom for full reads and automatic continuation, including an ordered batch
finalization check; ordinary previews retain normal context compaction.

Continuation must match the complete last-issued cursor. Reader-owned path
resolution preserves relative paths and aliases. The executed source must still
match the cursor snapshot. Page and measured active-time bounds stop dispatch;
no-progress advice changes the requested strategy, and repeated stalls pause.

Writers declare original ranges through their real preview/validation path.
`multi_edit` computes all changes against the original source for preflight.
An overwrite requires full evidence for the current raw version, including an
unsaved new buffer. The writer verifies the preflight source again at execution.
Same-batch reads never supply write evidence. Historical evidence failures are
re-evaluated against the frozen provider-round boundary and can be cleared.
Rebuild waivers require an exact path in the same affirmative rewrite clause.
Moves preserve content and reject existing destinations, so they require no
full-text evidence; anchored deletion retains its existing owner.

Desktop and CLI display disjoint one-based coverage and recovery guidance.
Generation and sequence fence stale frames; independent reads retain their
own entries and completion clears live status.

Regression coverage lives in `internal/agent/read_pipeline_regression_test.go`,
`internal/tool/builtin/read_evidence_regression_test.go`,
`internal/cli/read_status_test.go`, and the frontend read-status tests. Legacy
protocol and dependent-edit tests explicitly select rollback mode. The golden
provider request is updated for the intent/cursor schema already added by this
PR. This intentional schema change may rebuild the prompt cache on first use;
host envelopes and write-source checks remain outside provider-visible bytes.
There is no session-store migration. Old cursors must be re-read after a new run.

Local deterministic tests are distinct from native WebView2/WKWebView checks,
real-provider success/token measurements, exact-head remote CI, and release
availability. Those results must be reported separately.

## Completed-read reuse and terminal receipts

The follow-up separates historical requirement completion from original text
available in the frozen model request. The ordered tool finalizer associates
captures by workspace, canonical path, source kind, raw identity and snapshot.
A satisfied requirement stays satisfied on a verified repeat; expansion keeps
coverage and accounting and asks only for missing ranges. New runs own new
registries and cursor bindings.

Structured readers bypass generic string deduplication. A reference is allowed
only to identical original text in the actual sampled request, and only for
already covered ranges. References deliver no new source lines and cannot
reference other references. Projection removal or extension rewriting causes
bounded text delivery; it does not revoke an earlier completed requirement.
Extension changes that remove parseable source text also invalidate identity.

`incomplete_read` is a terminal pause outcome, not success or a transport retry.
The optional `read_pause` LocalOnly receipt preserves up to 32 affected files
and 64 ranges per field, without content or executable cursors. Desktop live
and history views use the same idempotent notice. Old sessions omit the field;
older readers ignore the sentinel through the existing LocalOnly tool identity.
No migration or user setting is required. Existing write preconditions remain
independent of this receipt. The follow-up changes no tool schemas or system
prefixes; re-delivering text removed by compaction can increase an individual
request's input usage.

The paid matrix retains the original 64 cases and adds 32 targeted cases plus
12 exact-write cases. Its HTTP relay caps all upstream attempts at 600, reserves
128,000 tokens before each request, retains reservations when usage is unknown,
and stops at 3 million tokens or four hours. The relay enforces a 2,048-token
output cap, including adapters that otherwise omit it, and credits known usage
even when a downstream client closes before the final SSE sentinel. It stores no credentials or request
bodies. Model task failures and host invariant failures remain separate results.

Native verification found that informational pause receipts were folded into
completed process material. Read pauses now remain outside the process fold,
after the preserved candidate response, without an implicit continuation action.
Pause outcomes also remain distinct from provider errors in desktop metrics and
telemetry. The same rendering regression covers the live/history notice shape.
The paginated TranscriptStore uses that same conversion, including empty-body
LocalOnly receipts; cold backend slices and repeated frontend page loads are
tested independently from the legacy full-history converter.
Read-status event ranges stay zero-based and half-open, matching envelopes and
pause receipts; CLI and desktop perform the one-based conversion only when
displaying them. The emitter regression prevents a double shift of the first
visible line found during native pagination verification.
