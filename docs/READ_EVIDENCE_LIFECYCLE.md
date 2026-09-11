# Read evidence lifecycle

Pagination, observed source versions and write authorization are separate facts.
This change follows up the read-evidence work in #9966/#9992 and the repeated-edit
reports #9994/#9995; it does not relax permission or sandbox boundaries.

## Runtime ownership

| Owner | Contract |
| --- | --- |
| `readcoord` | Tracks ranges, source snapshot, EOF and continuation budget. Ordinary `inspect`/`range` gaps do not block finalization; an attached Stop still does. |
| Observation ledger | Records delivered line hashes and sequence. Reads in the current provider batch cannot authorize its writes. Deduplication re-delivers text when the old observation predates the latest write. |
| Operation guard | Freezes each rejected operation's target, version, ranges/hashes and provider boundary. It never replays old replacement arguments to clear a rejection. |
| Operation ledger | Owns one intended change end to end: the reads that justify it, the mutation that applies it, the verification that settles it, and a bounded recovery budget for its failures. |

## Operation lifecycle

An operation's identity comes from what a call intends to do — its real target
and arguments — not from the provider's per-round call ID, so the same edit
resubmitted under a new ID is recognizably the same operation. States are
`prepared`, `applied`, `verification_pending`, `settled`, `failed`, `unknown`
and `needs_user`; `settled` and `needs_user` are terminal and never transition
twice. Ordinary work settles on the real tool result. Only a delivery floor
holds a change open until a verification covering its paths passes.

The same operation failing the same way twice is a loop, not a
self-correction: the host stops offering automatic recovery, moves it to
`needs_user`, and refuses to run it again. Real new information — a changed
source version, fresh evidence, a successful write superseding the source, or a
new user turn — opens a new recovery epoch. Only host rejections spend this
budget; a tool that ran and reported a real failure is information, and stays
with the existing repeat-failure and loop guards.

## Receipts and source tokens

Every successful tool result carries the host's own receipt ID
(`[receipt r_1a2b3c4d]`), and a `read_file` result carries its source token
(`[source_token r_…]`) — the same ID, usable as the handle for the version it
showed. `complete_step` accepts `receipt_ids`, and `edit_file`, `write_file`
and `multi_edit` accept an optional `source_token`.

Citing an ID is exact. Matching the command text a model retyped is not: a
dropped `cd` prefix, a different quote style, a reordered flag or another
working directory rejected verifications that had really run. Command text
remains a compatibility path for citations that name no ID, and is only ever
used for display otherwise.

A cited source token is resolved against the host's own record. A token naming
another file, an older snapshot, a window that covers only part of what the
write replaces, or nothing the host issued proves nothing and is rejected once
with the concrete recovery. Citing is optional: with no token the existing
snapshot matching stays authoritative, so the ordinary read-then-edit path is
unchanged.

Only the explicit tool argument `intent=full` creates a whole-file finalization
requirement. Natural-language claims are not mechanically verified or parsed
into new obligations: an ordinary successful final answer is not proof of a
complete review. A full requirement must reach verified EOF or pause within the
existing budget. A targeted strategy receipt does not prove a full read.

An earlier rejected operation is retired when its evidence is satisfied, a fresh
observation establishes a different version or confirmed absence, or a completed
write supersedes it. Future writes independently check their own current target.
Memo keys include call ID, tool name, arguments and observation boundary; memos
expire with the batch. Cleanup removes only the identical requirement key.

## Writer boundaries

- Edits use their actual preview's affected ranges and source identity, then
  check that identity again during execution. Unversioned bounded windows can
  prove individual ranges by current hashes, but cannot be stitched across
  versions or establish a full-file overwrite.
- Full-file replacement requires complete current evidence or the existing
  host-recorded rebuild authorization. Creation binds to confirmed absence;
  a file appearing between preflight and execution is not overwritten.
- Existing anchored deletions retain their anchor audit. No `delete_file` tool
  is added. Moves preserve bytes, including binary files: they check a host-
  observed source identity without requiring textual coverage. Destination and
  platform move checks remain native. This does not promise filesystem-wide
  atomic CAS against arbitrary external writers after the last identity check.
- Plain metadata-only `git commit -m` does not owe file-content evidence.
  Content-changing forms remain conservative. The shared classifier recognizes
  `git --no-pager diff/status/log`; redirects, external diff and arbitrary `-c`
  overrides receive no read-only exemption.
- Only literal `echo`/`printf` output redirects have a proven shell write scope
  here. Disjoint targets do not inherit another file's block. Scripts, dynamic
  expansions, glob targets, chains, hooks and unknown scopes remain opaque.
- A missing-evidence preflight failure permits an already-approved disjoint
  single-file writer in the same batch. Executed failures, hooks, ambiguous
  scopes and dependent verification retain the normal dependency barrier.

## Recovery and compatibility

Diagnostics use `READ_PARTIAL`, `READ_CURSOR_INVALID`, `READ_SOURCE_CHANGED`,
`READ_HARD_STOP`, `WRITE_EVIDENCE_MISSING`, `WRITE_EVIDENCE_STALE`,
`WRITE_TARGET_ABSENT`, `WRITE_TARGET_AMBIGUOUS`, `VERIFICATION_RECEIPT_MISSING`,
`VERIFICATION_RECEIPT_MISMATCH` and `OPERATION_NEEDS_USER`. They carry available path, operation,
version/range and recovery information, never file content.

A rejection is machine-executable rather than prose: it names the receipt IDs
that exist, the closed set of actions the host accepts
(`use_receipt:<id>`, `reread_target`, `run_verifier`, `mark_manual`,
`abandon_edit`) and the remaining retry budget. The model selects an action; it
never has to guess which wording the host will take. When the budget is spent,
the operation is reported to the user with its next action
(`continue_verification`, or `resolve_with_user` for a paused one) instead of
being sent back to the model.

Outside a delivery floor `complete_step` is a note: evidence is optional, and
anything the host cannot confirm is reported alongside the sign-off rather than
rejected. Argument shape is still validated. A successful command the host does
not recognize as a standard verifier is reported as unclassified in both modes —
projects verify through Makefiles, wrappers and private scripts — while the
delivery gate independently still requires a recognized verification before
changed work can finalize.

| Data | New reader of old data | Previous reader of new data |
| --- | --- | --- |
| Read envelope v2 | Existing meanings preserved | Protocol unchanged |
| Pagination text | Old trailers and `PARTIAL view` accepted | Display-only text |
| Optional `tool_diagnostic` | Missing is safe | Unknown optional field ignored |
| LocalOnly `read_completion` | Missing is safe; diagnostic only | Existing orphan sentinel prevents provider replay |
| Read status verdict, pause code/snapshot | Existing State/Reason still usable | Optional fields ignored |
| Rejected operations and preflight memos | New Run starts empty | Not persisted; no migration |
| Operation ledger, receipt IDs, source tokens | Turn-scoped; a new turn starts empty | Not persisted; no migration |
| `complete_step.receipt_ids`, writer `source_token` | Optional; omitting them keeps the previous behaviour | Unknown optional property ignored |
| Diagnostic recovery fields | Missing is safe | Unknown optional fields ignored |

`partial_read_sufficient` means finalization was permitted, not that the host
verified the model's understanding. Canonical coverage receipts are diagnostic
only and never authorize a resumed write. Model and compaction projections strip
these fields. Complete short reads retain their bytes. Partial-result and
append-only hint text changes. `complete_step` no longer requires `evidence`
and gained `receipt_ids`; the file writers gained an optional `source_token`.
Those schema edits change the stable system prefix once, on upgrade; within a
session the prefix stays byte-stable.

## Observability

Transitions publish content-free counters to any sink that wants them:
`operation_settled_total`, `operation_needs_user_total`,
`operation_recovery_attempt_total`, `operation_duplicate_block_total`,
`verification_auto_attached_total`, `verification_unclassified_total`,
`read_source_changed_total` and `complete_step_optional_call_total`. They carry
host identifiers only — never a path, an argument, a command, or tool output.
The ones that answer whether this worked are the average recoveries per
operation, the `needs_user` share, and the unclassified-command share.

## Validation

Regressions cover three repeated edit/read/retry cycles, changed anchors,
confirmed deletion, source/presence races, frozen batch boundaries, disjoint
writes, opaque shell blocking, partial/full finals, Stop priority and metadata
projection. The real Build regression adapted from #9992 stages a disposable
file, reads a large file, commits and verifies the actual Git commit.

Local tests, race checks, lint and cross-compilation are separate evidence from
remote CI, native Windows interaction and live-provider qualification.
