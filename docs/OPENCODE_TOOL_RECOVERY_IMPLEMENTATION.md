# Tool reliability and explicit protocol recovery

Validation date: 2026-09-05. This extends the existing replay projection, recovery budgets, independent search and file-write verification. It does not change the selected model, endpoint, protocol or thinking setting. No release, commit or publication is included.

## Behavior

- An empty/model-only HTTP 400 is classified as `upstream_reason_missing`. Users see that the upstream rejected the request without a specific cause, rather than a guessed reasoning failure. Structured diagnostics carry classification, HTTP status and a restricted trace token, never the response body. Existing verified replay errors retain automatic repair.
- Desktop exposes **Recover from valid history** outside the collapsed work section. CLI exposes `/recover-context [incident-id]`. Serve accepts `{ "action": "protocol_recovery", "recoveryId": "…", "input": "optional guidance" }`; ACP uses the same action/token on `session/prompt`. Recovery guidance is not a management command. The Controller owns admission on all transports.
- An action is offered only when the failed request has a changeable history projection. Credentials, quota, explicit parameter errors, unchanged history and spent repair incidents do not offer it. Session/model/endpoint scope, incident identity and history fingerprints are checked again at admission. Active-run admission rejects concurrent requests.
- A versioned local record distinguishes `pending`, `consumed` and expired input. Automatic and explicit repair share its accounting. Request preparation runs before consumption; the consumed checkpoint must be saved before a provider request starts. A checkpoint failure stops the request. An uncertain checkpoint write leaves memory conservatively consumed. Restart reads durable state and never starts a request automatically.
- **Consumption is separate from projection.** A plain regeneration consumes a repair attempt without enabling a new history projection. Actual repaired prefixes are restored after restart; healthy messages appended after the repaired boundary retain their original replay. A newly produced reasoning/tool round can create a new incident; repeatedly continuing the same failed prefix cannot renew its repair budget.
- Fault projection restores execution states from canonical local receipts by call ID and tool name. Not-started and unknown outcomes are preserved; reused IDs remain conservatively unknown. Receipt changes invalidate pending actions without renewing consumed budgets. These states do not enter healthy provider requests.
- Invalid JSON/schema/required fields use existing pre-execution validation and concrete error results. Corrected calls are allowed; completed calls in the same batch are not re-executed by the scheduler. Actual tool calls are processed even when the terminal finish reason is `stop`. Clean conversation still ends naturally.
- Existing permission denial, cancellation and unknown-write rules remain authoritative. A cancelled recovery discards late assistant output and tool dispatch. File recovery uses existing recorded write evidence; no new shell/MCP replay permission is introduced.
- Completed search results now have optional `sources_status`: `available` or `not_provided`. Missing sources are an informational completion state, preserve the summary, and never trigger another search. Generated prose URLs are not promoted to structured citations. CLI, Desktop and Serve display the missing-source notice. Old records without the field remain unspecified. Native-search display metadata is removed at provider projection; raw native search/Responses items remain unchanged. Independent tool JSON includes source status for both the model and the UI.
- Responses clients now honor explicitly configured `reasoning_protocol=deepseek` or `mimo` on compatible gateways. This selects the existing replay obligation without importing host-specific defaults, headers or limits. An unknown endpoint is not made strict solely by its model name. Healthy missing-item compatibility remains available.

## Kimi prompt decision

A fixed Kimi-only candidate is retained for live evaluation in `internal/config/model_action_policy.go`. It is **not enabled in production assembly**. Both main-agent and tool-subtask production prompts therefore remain unchanged, as do frozen assembly, non-Kimi prompts and auxiliary search prompts. No new user setting was added.

The candidate asks for actual permitted tool use, evidence-based completion and concrete error correction, while preserving read-only/plan restrictions, denial/cancellation and unknown outcomes. Recognition uses the actual model ID or existing Kimi contract, not the display name. The adoption gate remains closed because one avoided failure is insufficient evidence of reliable improvement and aggregate token usage rose.

## Live Kimi comparison

OpenCode Go `kimi-k3`, Chat protocol, low/max effort, three fixture operations, ten trials per profile in every cell: **120 completed trials**. Baseline uses the existing default/system policies; the candidate appends only the fixed instruction. Concurrency was four. Files and marker values were isolated and generated per trial. Verification launches only a fixed verifier with no provider credentials in its child environment. This runs Reasonix against Go; it is not an OpenCode executable benchmark.

| Effort / operation | Baseline completed | Candidate completed | Requests baseline / candidate | Prompt + output tokens baseline / candidate |
|---|---:|---:|---:|---:|
| low / read | 10/10 | 10/10 | 20 / 20 | 12,262 / 13,702 |
| low / edit | 10/10 | 10/10 | 22 / 21 | 18,794 / 19,656 |
| low / verify | 10/10 | 10/10 | 20 / 20 | 12,121 / 13,823 |
| max / read | 10/10 | 10/10 | 20 / 20 | 12,109 / 14,181 |
| max / edit | 10/10 | 10/10 | 20 / 20 | 16,884 / 18,921 |
| max / verify | 9/10 | 10/10 | 19 / 20 | 11,551 / 14,295 |

Baseline: 59/60 tasks, 61 tool proposals, 59 actual successful fixture operations, 121 requests, 72,532 prompt + 11,189 output tokens. Candidate: 60/60 tasks, 61 proposals, 60 successful operations, 121 requests, 83,092 prompt + 11,486 output tokens. No fixture was successfully executed twice. First proposed arguments were correct in all tasks with a proposal (59/59 and 60/60); coverage including no-call tasks was 59/60 and 60/60. Two edit cases made additional unsuccessful proposals and still completed with one successful write; these are not counted as network retries. No initial-invalid-call sample occurred, so live correction probability for that subgroup is unmeasured; deterministic tests cover it.

The baseline failure was a clean final response without any tool proposal in max/verify, retained as a failure. No manual continuation was used. Candidate aggregate tokens were 13.0% higher. Mean trial duration was 9.07s baseline / 8.93s candidate, with shared-service variability; this is not a latency improvement claim. An earlier partial run was stopped to improve argument accounting and excluded from the final 120.

## Live protocol/search observations

The replay fixture corrupts only the second outbound tool-round request. Canonical history retains real results. HTTP rejections below are actual server responses, not injected HTTP status codes.

- Official DeepSeek Flash/Pro × Chat/Anthropic/Responses: six cases recovered from actual 400s. Four satisfied the one-tool acceptance criterion. Flash Chat and Flash Responses each requested the read-only echo again, producing a second execution: **two retained failures**, despite successful protocol recovery. Recovery facts are guidance; they do not guarantee that a model never requests a fresh read. No permission or write guard was relaxed to make these pass.
- Go DeepSeek Flash/Pro × three protocols: both Anthropic cases initially stopped on an opaque 400 and **explicit recovery succeeded 2/2**, with one tool execution each. Manual recovery is reported separately from initial success. Flash Chat's corrupted request was accepted by the upstream and is not evidence of rejection recovery. Pro Chat/Responses automatically recovered. The first Flash Responses run exposed the missing explicit replay-contract handling and failed.
- After that Responses fix, the same Go Flash Responses rejection case passed **3/3**, each with statuses `[200,400,200]`, three requests and one tool execution. The original failure remains in the report. These three cases used 3,034 prompt and 1,753 output tokens in total.
- Go native search: Flash/Pro × Anthropic/Responses **4/4** search-and-tool continuations passed. Anthropic returned ten structured sources per case. Responses returned zero structured sources, both marked `not_provided`; completed raw items replayed unchanged on the next request. Total: eight requests, 73,234 prompt + 1,299 output tokens. No substitute search was attempted.

These results do not close every custom-endpoint scenario in issue #9808, and they do not establish universal exactly-once model behavior. LongCat/Zhipu were not rerun for this narrower change; their earlier results remain in `MULTIPROVIDER_VALIDATION.md`.

## Local verification and reproduction

- Root `go test -p 2 ./...`; Desktop module `go test ./...` (separate modules).
- Focused race coverage of protocol/manual recovery, cancellation/generation, replay, argument validation, search and existing write recovery. A channel-controlled test releases a provider response after cancellation and asserts that no tool or assistant result commits.
- Durable Controller tests read the saved consumed record while the recovery provider is blocked, before releasing it; repeated and concurrent actions cannot reach the provider. Additional cases cover stale/new-input tokens, restart, unknown versions/fields, preparation cancellation and checkpoint failure.
- Frontend type checks, all 255 discovery-based suites and focused interaction tests; actual browser clicks exercise the production transcript/button/tool-card components using the mock bridge. Browser coverage checks recovery, stop, no late search, missing-source notice and retained summary. It does not validate native Wails window behavior. The existing mock sidebar emits duplicate-tab-key console warnings; there were no page exceptions in the successful recovery scenario.
- Run the browser scenario with `node desktop/frontend/bench/protocol-recovery.mjs` (Chrome installed). Live tests use `-tags live` and environment credentials: `TestLiveKimiActionComparison`, `TestLiveManualProtocolRecovery`, `TestLiveMultiProviderNativeSearch`. Test binaries and captured outputs must not contain credentials.

The new local sentinel uses raw JSON to preserve unknown versions/fields on round-trip; unknown versions are not actionable. Older clients can read ordinary history but cannot enforce the new repair accounting. Do not depend on a downgrade to continue an unresolved incident safely. Requests without search or recovery keep their provider prefixes and tool schemas. Deliberate fault-history projection can invalidate the affected cached prefix. The new independent-search JSON field changes that tool result and its subsequent prefix, without changing the preceding system text or tool schemas. Source status is not native replay proof.

## Tool checkpoint persistence and CI budgets

Tool receipts still commit the canonical transcript, revision and event index before execution continues. Append checkpoints defer derived display/listing projections to normal snapshots; rewrites refresh them immediately and publish a history invalidation. Restarted sessions rebuild pending projections from canonical history. This avoids rebuilding large display indexes for every tool result without weakening the write-before-execution boundary.

The 121-round regression uses the same writer authority as production; it retains a five-second no-progress watchdog and a thirty-second total test bound, since full-suite/race disk contention can exceed the old five-second whole-turn allowance. The 121-request assertion is unchanged. Same-environment frontend builds measure initial gzip 465.4 to 466.6 KiB and raw payload 2480.9 to 2484.5 KiB. Recovery/source-status locale copy measures 60.927/61.789 KiB (zh/zh-TW). Only these measured budgets receive bounded headroom; CSS and individual chunk budgets remain unchanged.

### CodeQL context-flow triage

PR analysis 1729619378 reported 20 `go/path-injection` alerts: 248, 250, 251, 254–259, and 316–326. All have the same alert IDs and sink fingerprints in base analysis 1729489499 (`e47ff8cb63916616b05086caadb3d7e10fd6b442`). Reviewing all 80 reported flows shows user input/format crossing `context.WithValue` into a different private key: parent session, job session, job manager, or temporary-directory manager. The string cannot replace those values; manager getters additionally assert distinct pointer types. Runtime-policy values use another private key and cannot mutate these owners either.

`TestRawInputCannotReplacePathOwningContextValues` exercises traversal/absolute-path payloads, both context layering orders, response format, policy strings, and missing owners. It verifies exact manager identity and unchanged session ownership. This evidence scopes false-positive triage to these existing alerts; it neither disables CodeQL nor exempts path injection elsewhere.

Integration with main-v2 `e5cf58daa` preserves the rich-link menu and keyboard fixes. The combined build measures 466.905 KiB initial gzip, 2485.715 KiB raw, and 61.027/61.881 KiB locale chunks; the corresponding bounded ceilings are 467.0, 2485.9, and 61.1/62.0 KiB.

Windows CI additionally exposed a topic-state timing bug: the mutation deadline started before cold SQLite open/reconciliation, which used a different context. The unchanged five-second mutation budget now starts after preparation. Deterministic regressions verify phase ordering, context cleanup and rejection of cancelled writes.

The next Windows run exposed an unbounded test wait before the finalize callback. Its original early-return error was discarded, so that run alone does not identify the underlying error. A deterministic local probe reproduced a real failure mode in the same multi-root lease path: two independent roots can hash to one tree-lock stripe, and sequential acquisition waits on itself. Merge-back now acquires a stably ordered union of compatibility locks and deduplicated tree stripes, preserving legacy and independent-owner exclusion. Tests cover collisions, nested roots, cancellation rollback and release; admission tests observe early errors and cancel/drain workers before restoring hooks. Workspace-lease and finalize-admission race suites pass ten repetitions.
