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
`WRITE_TARGET_ABSENT` and `WRITE_TARGET_AMBIGUOUS`. They carry available path,
operation, version/range and recovery information, never file content.

| Data | New reader of old data | Previous reader of new data |
| --- | --- | --- |
| Read envelope v2 | Existing meanings preserved | Protocol unchanged |
| Pagination text | Old trailers and `PARTIAL view` accepted | Display-only text |
| Optional `tool_diagnostic` | Missing is safe | Unknown optional field ignored |
| LocalOnly `read_completion` | Missing is safe; diagnostic only | Existing orphan sentinel prevents provider replay |
| Read status verdict, pause code/snapshot | Existing State/Reason still usable | Optional fields ignored |
| Rejected operations and preflight memos | New Run starts empty | Not persisted; no migration |

`partial_read_sufficient` means finalization was permitted, not that the host
verified the model's understanding. Canonical coverage receipts are diagnostic
only and never authorize a resumed write. Model and compaction projections strip
these fields. Tool schemas and stable system prefixes are unchanged; complete
short reads retain their bytes. Partial-result and append-only hint text changes.

## Validation

Regressions cover three repeated edit/read/retry cycles, changed anchors,
confirmed deletion, source/presence races, frozen batch boundaries, disjoint
writes, opaque shell blocking, partial/full finals, Stop priority and metadata
projection. The real Build regression adapted from #9992 stages a disposable
file, reads a large file, commits and verifies the actual Git commit.

Local tests, race checks, lint and cross-compilation are separate evidence from
remote CI, native Windows interaction and live-provider qualification.
