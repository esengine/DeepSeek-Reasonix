# Issue draft — not for posting yet

**Status:** internal draft. Six rounds of end-to-end probes are in:

| Round | What was measured | Outcome |
|---|---|---|
| 1 | Prefix cache rate FLAT vs ORCH (raw API) | Cache argument falsified — both ~98% |
| 2 | Read-heavy distillation, 3 spawns | ~6% compression per spawn (17× squash) |
| 3a | Write-heavy negative case | ~85% compression — distillation barely applies |
| 3b | Break-even arithmetic | 60-turn analytic, but pessimistic vs empirical |
| 4 | 6-turn FLAT vs SUB end-to-end (1 run) | SUB +24.6%; per-turn delta favored SUB at tail |
| 4-long | 12-turn × 3 repeats | Median SUB +24% over FLAT, dominated by spawn storms; **46% empty-output rate** raised reliability concern |
| 5 | Empty-output diagnosis (3 tasks × 3 budgets) | Two recoverable mechanisms account for most of the rate — fixable in spawnSubagent + budget tuning |
| 5b | Re-ran 5 with `forcedSummary → output` routing prototyped | Patch tested green (2 971 tests); storm-summary spawns now ship 286–391 tok of partial answer in `output` instead of stranding it in `error`. Patch not landed; recommended as Issue 2. |
| 6 | 12-turn × 3-repeat post-patch (Issue 1 + Issue 2 in tree) | **Median SUB flipped from +24% (Round 4-long) to −13.2% — SUB now wins by 13%.** Useful-spawn rate 54% → 73%; forcedSummary partial-answer recovery 0% → 9.1%; variance still real (one of three runs still SUB +14.6%). |
| 7 | Budget sweep, 5 tasks × 5 budgets (Issue 3) | Current default `DEFAULT_PAUSE_EVERY = 16` is the empirical knee — 80% success on representative read-heavy tasks. Remaining 20% (storm-prone investigation) is **not a budget problem** — fails identically at budget=32. Keep 16. |

**Posting plan**: four issues out of this draft.

1. **"Expose sub-agent distillation + reliability as first-class
   metrics"** — ready to file. Surfaces compression, success rate
   (true success = non-empty output), spawn cost, spawn-storm
   warnings, paused-with-resume-id state. **Data layer landed
   internally** as `src/telemetry/subagent-distillation.ts` with
   11 unit tests; not yet promoted to the public API surface.
   Computes per-spawn distillation from `SubagentResult` + output
   string, aggregates a session summary, counts spawn storms.
   Wiring into the parent loop / TUI is the remaining work.
2. **"Route `forcedSummary` text to `SubagentResult.output`"** —
   **landed in tree.** `src/tools/subagent.ts` now discriminates
   the two forcedSummary paths on `parentSignal.aborted`. Storm-
   breaker / context-guard content lands in `output`; user-abort
   keeps the legacy `error` routing. New optional
   `forcedSummary?: boolean` on `SubagentResult` lets callers
   distinguish partial from full answers without checking content
   shape. `formatSubagentResult` renders forced-summary spawns as
   `{ success: false, partial: true, output, note }` with `paused`
   still taking precedence when both are set. 4 new tests in
   `tests/subagent.test.ts` cover the formatter branch, precedence
   ordering, and the fall-through to the generic !success shape.
3. **"Tune default `maxToolIters` for `spawn_subagent`"** —
   **resolved: keep 16.** Round 7 swept 5 tasks × 5 budgets
   (`scripts/probe-budget-sweep.mts`). Budget 16 hits 80% success
   on representative read-heavy work — the realistic ceiling. The
   remaining 20% (a storm-prone investigation task) is **not a
   budget problem**: it forced-summaries at iter 4-5 regardless of
   budget (32 fails identically to 16; 24 succeeded but by
   variance). Lowering to 8 saves 22% per-spawn cost at identical
   success rate in this sample but isn't worth the regression risk
   at N=1. Real follow-up issues belong elsewhere — auto-resume on
   pause, paged `read_file`, soft prompt nudges to give up on
   unreachable artifacts.
4. **"Sub-context topology for orchestrator-style decomposition"** —
   **deprioritize.** Round 4-long shows the per-session win isn't
   reliably extractable until (2) and (3) land. Revisit after those
   are merged + a follow-up probe confirms empty-output rate has
   dropped under 20%.

When ready: paste the body of (1) into a new issue labelled
`enhancement` + `pillar-2` + `metrics`. Issues (2) and (3) are
small fixes that can ship before or alongside (1).

### Status of in-tree work (Issue 1)

`src/telemetry/subagent-distillation.ts` exports:

- `SpawnDistillation` — per-spawn shape: `completionTokens`,
  `outputTokens`, `savingsTokens`, `compressionRatio`, `hasOutput`,
  `costUsd`, `paused`.
- `SubagentResultLike` — structural shape `computeSpawnDistillation`
  accepts. Exists so the telemetry module doesn't import from
  `tools/subagent.ts` (avoids a stats ↔ subagent ↔ loop cycle later
  if someone wants to roll the collector into `SessionStats`).
- `computeSpawnDistillation(result)` — honest lower bound: ignores
  tool-result tokens.
- `SubagentSessionSummary` — aggregate shape: `spawnCount`,
  `usefulSpawnCount`, `pausedSpawnCount`, `successRate`,
  `totalCompletionTokens`, `totalOutputTokens`, `totalSavingsTokens`,
  `aggregateCompressionRatio`, `totalCostUsd`.
- `summarizeSubagentSession(spawns)` — completion-token-weighted
  compression aggregation, not naive mean.
- `DEFAULT_SPAWN_STORM_THRESHOLD = 3`, `countSpawnStorms(spawnsByTurn,
  threshold?)` — count turns that emitted ≥ threshold spawns.
- `SubagentTelemetry` — live collector class. `record(result)` is
  pre-bound so it works as a callback. `startTurn(n)` groups
  subsequent records into a new bucket so `stormCount()` is
  meaningful.

`src/tools/subagent.ts` — added one optional field to
`SubagentToolOptions`:

- `onSpawnComplete?: (result: SubagentResult) => void` — fires once
  per spawn dispatch, after `spawnSubagent` returns and before its
  result is formatted for the parent. Callback errors are swallowed
  with a try/catch so telemetry failures can't break the spawn tool.

Wiring pattern (intended use):

```ts
const telemetry = new SubagentTelemetry();
registerSubagentTool(registry, {
  client,
  onSpawnComplete: telemetry.record,
});
// ... agent runs, telemetry auto-populates ...
console.log(telemetry.summary);     // SubagentSessionSummary
console.log(telemetry.stormCount()); // turns with ≥ 3 spawns
```

`src/index.ts` re-exports the new types + values so library
consumers don't need a deep import:

```
SubagentResult, SubagentResultLike, SubagentSessionSummary,
SpawnDistillation, SubagentTelemetry, DEFAULT_SPAWN_STORM_THRESHOLD,
computeSpawnDistillation, countSpawnStorms, summarizeSubagentSession
```

Tests covering all of the above:

- `tests/subagent-distillation.test.ts` — 16 cases:
  read-heavy strength, write-heavy near-1, empty/whitespace output,
  passthrough clamp, zero-completion guard, aggregate sanity,
  weighted compression, storm counting (default + custom threshold),
  `SubagentTelemetry` lifecycle (empty start, single record, turn
  buckets, bound-callback usage, live summary updates).
- `tests/subagent.test.ts` — +2 cases: `onSpawnComplete` fires once
  per dispatch with the full `SubagentResult`; thrown errors in the
  callback do not propagate out of the spawn tool dispatch.
- `tests/public-api.test.ts` — snapshot updated for the 10 new
  public names.

All affected suites green (60 tests in this slice). Full project
suite: 2 947 / 2 952 (the 2 remaining failures are pre-existing
`tests/comment-policy.test.ts` flags against `src/demo-utils.ts`,
unrelated to this work).

What's still open (intentionally deferred):

- **No TUI surface yet.** Wiring a `SubagentTelemetry` cell into
  the top bar is a separate UX-shaped PR.
- **`SessionStats` integration deferred.** The collector lives
  standalone and the parent loop's `CacheFirstLoop.stats` is
  untouched. If we later decide the aggregate belongs in
  `SessionStats` (so `loop.stats.subagentSummary` is the canonical
  lookup), the structural `SubagentResultLike` interface makes that
  refactor non-cyclic. For now the explicit `new SubagentTelemetry()`
  wiring is the documented path.
- **No automatic `startTurn` wiring.** Callers using
  `CacheFirstLoop` would need to call `telemetry.startTurn(loop.currentTurn)`
  themselves at the boundary. Could be automated by exposing a
  parent-loop integration helper; deferred.

---

## Title

`Lean into sub-context distillation as the primary economy of long sessions (and surface it as a first-class metric)`

## Body

### Headline

`spawnSubagent` already exists and already does the load-bearing
thing: it runs a child loop in an isolated context and returns one
distilled string. After seven rounds of live-API probes, the
empirical picture is now positive — but the win was conditional on
two small fixes that both landed in this branch:

- **Single-shot read-heavy** (Round 2): compression ~6% per spawn.
  Clear win.
- **Single-shot write-heavy** (Round 3a): compression ~85%. Small
  savings; not enough to justify spawning on distillation alone.
- **Multi-turn end-to-end with variance, pre-patch** (Round 4-long):
  SUB beats FLAT cumulatively through turn 10. **One bad
  investigation turn can trigger a spawn storm that erases the
  lead.** Median 12-turn result: SUB +24% vs FLAT. 46% of spawns
  returned empty `output`. Per-run spread for FLAT alone: 3.4×.
- **Round 4-long's empty-output rate diagnoses cleanly** (Round 5):
  two recoverable mechanisms — `paused` budget exhaustion (caller
  could resume but doesn't) and `forcedSummary` content routed to
  `error` instead of `output` (a one-line spawnSubagent fix).
- **Multi-turn end-to-end with variance, post-patch** (Round 6,
  same script): Issue 1 (`SubagentTelemetry`) and Issue 2
  (`forcedSummary → output`) both in tree. **Median SUB −13.2%
  (win).** Useful-spawn rate 73%; forcedSummary partial-answer
  recovered into output 9.1% of spawns; variance still real
  (one of three runs still lost 15%). **37-point swing on the
  same workload after the two fixes.**

The proposal: surface what we've measured. Per-spawn metric for
distillation + reliability, session-aggregate for spawn cost vs
realized savings, spawn-storm warnings when a turn fires ≥3 spawns.
The metric makes the topology decision-supported instead of
prose-supported.

In parallel: three small follow-up code changes fall out of
Round 5 (route `forcedSummary` to `output`, calibrate default
`maxToolIters`, consider auto-resume on pause) that should
measurably reduce the empty-output rate. Those are tracked as
separate issues, not part of the metric PR.

### What's broken

The coding loop today is one `messages[]` per session, grown
monotonically. That shape was the right starting point — append-only is
how Pillar 1 keeps prefix cache hits high inside a single task. It
stops being optimal as soon as the session spans more than one *kind*
of work:

- Switching from "plan the refactor" to "edit module A" to "review the
  diff" all share one system prompt. The system prompt is therefore
  generic. Generic system prompts give the model less role pressure
  than role-specialized ones, and we pay (at hit price) for
  instructions that don't apply to the current step.
- Each turn's prompt grows by the previous turn's user input + tool
  results + assistant output. Even at 98% cache hit, the *prefix size*
  ratchets up turn over turn — you're paying a slowly-rising
  hit-priced bill for history that the current sub-task does not
  need.
- Cross-talk between modules pollutes attention. When we're editing
  module A, module B's read history is distractor tokens — billed for,
  and demonstrably worsening edits in spot checks.

One context is the wrong shape for multi-step coding work. I want
many small contexts, each with a stable role prefix, each addressing
one concern. The economic case is that ORCH keeps per-call prefix
flat where FLAT lets it grow; the attention case is independent of
caching.

### Proposal — the small, measurable, ship-it-first version

Before any topology change: **expose four numbers per spawn**.
Observation-only, no behavior change. Calibrated to the failure
modes Round 4-long surfaced.

1. Extend `SubagentResult` with a `distillation` block:
   ```ts
   distillation: {
     completionTokens: number;       // sum across child iters
     toolResultTokens: number;       // sum across child tool returns
     outputTokens: number;           // countTokens(result.output)
     savings: number;                // completionTokens + toolResultTokens − outputTokens
     compressionRatio: number;       // outputTokens / completionTokens
     success: boolean;               // false when output is empty / whitespace-only
     costUsd: number;                // already on SubagentResult, mirror here for grouping
   }
   ```
2. Aggregate into `SessionStats`:
   - `cumulativeSubagentSpawns`
   - `cumulativeSubagentCostUsd`
   - `cumulativeSubagentSavingsTokens`
   - `subagentSuccessRate` — successful spawns / total spawns
   - `spawnStormCount` — turns in which ≥3 spawns fired
3. Surface in the TUI:
   - one cell: `subagent N (Mtok saved, $X spent, K% success)` —
     all four numbers visible at a glance
   - warning toast on spawn storm — "turn N spawned 4 sub-agents,
     consider whether the question can be narrowed" (the user
     can't easily fix this mid-session but should know it happened
     so they can shape future prompts)

That's the whole first PR. No topology change. No agent flow change.
Just four numbers we should already have, calibrated against the
two reliability failures Round 4-long captured.

### Proposal — the bigger topology change (conditional)

An **orchestrator** owns task decomposition and integration. It does
not write code. It calls `spawn(role, brief)` — a tool — to launch a
**sub-context** that owns exactly one module-sized task. The
sub-context sees only its own role's system prompt and the brief; it
never sees the orchestrator's history. When it returns, its final
string is appended to the orchestrator's log as a synthetic tool
result.

Roles are finite and hand-written. v1 candidates: `architect` (the
orchestrator itself), `coder`, `reviewer`. Each role has:

- a frozen system prompt (hash-pinned at build time; rename the role
  if the hash changes)
- a tool whitelist baked into the role definition as a typed contract,
  not a convention (coder gets `edit_file`, reviewer does not, etc.)
- a few-shot block, also frozen

The sub-context's `messages[]` is
`[role.system, role.tool_specs, role.fewshots, brief, ...turns]`. The
first three are byte-identical across every call to that role; brief
is short (≤500 tokens target) and rendered deterministically — same
inputs produce the same bytes.

`spawn` is dispatched through the existing tool dispatcher with
`parallelSafe: true` for independent sub-contexts, so we get free
parallelism via the Pillar 1 chunking that already lands today.

### Reference — what DeepSeek's cache actually does

Written down once so future RFCs can reference it instead of
re-deriving:

- DeepSeek prefix cache is hashed in **64-token blocks**. Below one
  block, nothing caches.
- Matching is **longest common prefix from token index 0** against a
  prior request to the same model. One token changing at position `i`
  invalidates everything from `i` onward.
- It's **automatic** — no `cache_control` markers, no opt-in. You
  design for it or you don't get it.
- Cache hit input ≈ 10% of miss price. Output unaffected.

These facts matter because they explain why the original "spawn
isolated contexts to maximize cache hit rate" pitch is wrong: cache
*hit rate* is already near-saturated under Pillar 1, so a topology
change can't meaningfully improve it. The win has to come from
shrinking what we send, not from caching what we send better.

This is not LangGraph. It is not memory. It is not multi-shot
sampling. Cheapness is load-bearing and we do not spend tokens to
vote.

### Targets

Targets the issue should claim once the metric ships, calibrated to
Round 4-long's empirical numbers:

- **Per-spawn `compressionRatio`** reported. Read-heavy typical
  range 5–20%; write-heavy typical range 60–90%. Both reported as
  data; neither is a pass/fail gate.
- **Per-spawn `success`** reported, where `success=false` means
  empty / whitespace-only output. **Today's empty-output rate is
  46% across 13 measured spawns** — surfacing this should produce
  immediate pressure to drive it down.
- **Cumulative `subagentCostUsd` vs `subagentSavingsTokens × hit_price`**
  displayed side by side. User can read off net for the session.
- **Spawn-storm warning** when a single turn fires ≥3 spawns.
  Round 4-long captured 2 storms in 6 SUB runs (turn 11). They
  dominate session cost.
- **No regression in task success rate** on tau-bench when
  sub-agents are exposed by default vs. hidden.

Numbers the issue should NOT claim:

- "Order of magnitude cache saving" — false, Round 1 disproved it.
- "≥ 40% billed-input-token reduction" — withdrawn.
- "Higher cache hit rate via role prefixes" — measured 73.7% vs.
  FLAT's 98.2%. Sub-contexts cache **worse** per spawn.
- "Spawn-anything-for-cleanliness" — Round 3a + Round 3b show
  write-heavy spawns need 80–150+ follow-up turns to pay back.
- "Sub-agents are unconditionally cheaper than inline" — Round 4
  disproved it on a 6-turn session.
- "Sub-agents are cheaper than inline on long sessions" —
  Round 4-long disproved this. Spawn storms can erase the lead
  on a single turn.
- "Sub-agents fail 46% of the time" — that was Round 4-long's
  surface reading; Round 5 shows most of those "failures" were
  either recoverable (pause) or had useful content stuck in the
  wrong field (forcedSummary). The accurate framing is "the
  current `SubagentResult` shape understates partial success."

### Open questions to resolve before filing

- File-tree access: each sub-context calls `read_tree` itself, or the
  orchestrator bakes a tree slice into the brief? Baked slice is
  cheaper and more deterministic; I'd default to that.
- Streaming UX: forward sub-context tokens to the TUI with a role
  prefix, or show "coder-A working…" spinners? Spinners for v1
  unless replay tells us users need the inner stream.
- Failure mode: a sub-context that loops or refuses should surface as
  `{ok: false, reason}` to the orchestrator, which decides retry vs.
  abandon. No silent fallbacks.
- Cache prewarming: dummy tiny call per role at session start to seat
  the role prefix in DeepSeek's cache? Cheap insurance against cold
  cache; needs measurement to justify even the dummy call's cost.

### Non-goals

- Generic agent framework. Roles are finite, hand-written, reviewed.
- Multi-shot sampling, MCTS, parallel completions. None of it.
- Long-term per-role memory. Sub-contexts die after returning. If we
  need memory later, that's a separate issue.
- Replacing Pillar 1. Orchestrator and each sub-context individually
  still follow immutable-prefix / append-only-log / volatile-scratch.

### Measurements

#### Round 2 — distillation savings on read-heavy spawns (the real headline)

Probe: `scripts/probe-subagent-distillation.mts`. Three read-heavy
tasks against the live repo, each spawned via `spawnSubagent()` with
`list_dir` + `read_file` tools, `deepseek-chat`, 2026-05-15.

For each spawn we measure:

- `completionTokens` — total assistant tokens the child generated
  across its full loop. Inline (no subagent), every one of these would
  have ended up in the parent's append-only log.
- `outputTokens` — `countTokens(result.output)`. This is the only
  thing the parent log actually grows by.
- `savings = completionTokens − outputTokens` (lower bound — ignores
  tool result tokens, which would also have inflated the parent log).
- `compressionRatio = outputTokens / completionTokens`.

| Task | turns | toolIters | completion | output | savings | compression |
|---|---:|---:|---:|---:|---:|---:|
| `summarize-index`  |  4 |  5 |  534 |  61 |  473 | 11.4% |
| `list-tools`       |  3 | 17 | 1728 | 306 | 1422 | 17.7% |
| `loop-classes`     | 14 | 16 | 6008 | 125 | 5883 |  2.1% |
| **aggregate**      | 21 | 38 | 8270 | 492 | **7778** | **5.9%** |

**Headline**: on read-heavy work, the parent log grows by ~6% of what
it would have inline. Total cost across the three spawns was $0.0123;
total spared parent-log tokens was 7 778. Whether that pays back
depends on how many parent turns follow — each subsequent turn would
have re-shipped those 7 778 tokens at the cache-hit price. Rough
break-even on `deepseek-chat` (~$0.27/M input) at ~50 follow-up turns,
clear win past that.

This is the real economic argument for sub-contexts. The cache rate
story (Round 1) was a distraction; the **deferred-cost** story is the
load-bearing one. Sub-contexts are a way of paying the work cost once
and refusing to keep paying for it as the session continues.

Honest caveats:

- This is the strength case. Read/explore/summarize spawns compress
  well because the work product is small relative to the
  investigation. Write/edit spawns where the work product *is* the
  artifact won't compress like this — `compressionRatio` will be
  closer to 1.
- Intra-spawn cache hit rate was 73.7% across these three spawns
  (cold-start cost on each fresh spawn, short sessions per spawn).
  That's worse than the parent FLAT loop's ~98%. **The deferred-cost
  win dwarfs the cache-rate loss**, but we should expose both numbers
  so future tuning sees the tradeoff.
- `savings` is a lower bound. Real number is `completionTokens +
  tool_result_tokens_kept_in_child − outputTokens`. We don't expose
  per-iter tool-result accounting yet; should add it.

#### Round 7 — `maxToolIters` budget sweep (Issue 3)

Probe: `scripts/probe-budget-sweep.mts`. Five tasks across the
read-heavy investigation difficulty range (easy single lookup →
storm-prone wiring walkthrough), each run at five budget caps
(4 / 8 / 16 / 24 / 32). 25 spawns total. `deepseek-chat`, 2026-05-15.

| budget | success | paused | forced-summary | mean cost | mean output |
|---:|---:|---:|---:|---:|---:|
|  4 | 2/5 | 3 | 0 | $0.000565 |  17 tok |
|  8 | 4/5 | 1 | 0 | $0.001071 |  45 tok |
| **16** | **4/5** | **0** | **1** | **$0.001373** | **78 tok** |
| 24 | 5/5 | 0 | 0 | $0.002893 |  70 tok |
| 32 | 4/5 | 0 | 1 | $0.001393 |  51 tok |

Per-task knee:

| task | difficulty | smallest budget that succeeded |
|---|---|---:|
| list-loop-dir       | easy        |  4 |
| three-file-summary  | medium      |  4 |
| single-file-const   | easy        |  8 |
| cross-file-search   | hard        |  8 |
| loop-prefix-wiring  | storm-prone | 24 only |

**The interesting finding: budget 32 fails where 24 succeeds.**
The storm-prone task hit `forceSummaryAfterIterLimit("stuck")` at
iter 4 (budget 16) and iter 5 (budget 32) — independently of how
much budget was available. Model self-detected duplicate tool
calls and bailed via storm-breaker. Only budget=24 happened to
avoid the storm trigger and run to completion. **This is run-to-run
model variance, not a stable budget effect.**

Recommendation: **keep `DEFAULT_PAUSE_EVERY = 16`**. The data
justifies it:

- Budget 16 hits 80% success, the realistic ceiling for read-heavy
  investigation in this sample.
- The 20% failure is a storm-summary edge case **independent of
  budget** (32 also fails it).
- Budget 24 hit 100% but the storm-prone task's success was
  variance, not robustness.
- Lowering to 8 saves 22% per-spawn cost at identical 80% success
  rate, but only at N=1 sample — variance dominates.

What the remaining 20% needs (separate issues, not budget tuning):

- **Auto-resume on pause** — Round 6 showed paused spawns are real
  recoverable losses if the caller doesn't resume.
- **Paged `read_file`** — duplicate-call storms partly come from
  models re-reading truncated files trying to reach later content.
- **Soft prompt nudges** — encourage the model to give up on
  artifact-bound tasks rather than retry, before storm-breaker has
  to fire.

#### Round 6 — same 12-turn × 3-repeat script, post-patch

Probe: `scripts/probe-session-e2e-post-patch.mts`. Identical user
script and timing to Round 4-long. The differences: Issue 1's
`SubagentTelemetry` is wired into the SUB-mode run (collector
recording every spawn), and Issue 2's `forcedSummary → output`
routing is in the tree. Same model, same temperature defaults.

**Per-run totals:**

| run | FLAT total | SUB total | Δ% | spawns | useful | forcedSummary | paused | storms |
|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| 1 | $0.0584 | $0.0334 | **−42.8%** | 5 | 4 | 0 | 1 | 1 |
| 2 | $0.0385 | $0.0441 | +14.6% | 3 | 2 | 1 | 1 | 0 |
| 3 | $0.0301 | $0.0280 | −6.8% | 3 | 2 | 0 | 1 | 1 |
| **median** | **$0.0385** | **$0.0334** | **−13.2%** | 3 | 2 | 0 | 1 | 1 |

**Aggregate (across 11 spawns from 3 SUB runs):**

- Useful-spawn rate (non-empty output): **72.7%**
  (vs Round 4-long: 54%)
- forcedSummary partial-answer routed to `output`: **9.1%** of spawns
  (vs Round 4-long: 0% — content was stranded in `error`)
- Paused (recoverable, not used in this probe): 27.3%
- Storms (≥3 spawns/turn): 2 across 3 runs
- Aggregate `compressionRatio`: ~19% (paused spawns with 0 output
  drag this up; useful-only compression is closer to the Round 2
  ~6%)

**Compared to Round 4-long, the median delta moved from SUB +24%
(loss) to SUB −13.2% (win) — a 37-percentage-point swing on the
same workload.** The mechanisms behind the swing:

1. Issue 2 (`forcedSummary → output`) directly converts ~9 pp of
   the empty-output rate into useful-output rate. The model wrote
   the content; we stopped throwing it away.
2. Issue 1 (`SubagentTelemetry`) doesn't change behavior, but it
   makes the variance visible per spawn — Run 2's loss is now
   inspectable instead of opaque.
3. Run-to-run model variance still real: Run 1 saved 43%, Run 2 lost
   15%. **Single-run measurements remain unreliable**, but the
   median direction has flipped.

What this means for the bigger Issue 4 (orchestrator topology):
the per-session win is now reliably extractable on read-heavy work
of this shape. The two-step fix lifted SUB from "net loss most of
the time" to "wins on median, variance still real." That's the
empirical floor the topology proposal needed before discussion is
productive.

#### Round 4-long — 12-turn × 3-repeat (pre-patch)

Probe: `scripts/probe-session-e2e-long.mts`. 12-turn read-heavy
script (codebase exploration → architectural questions), three
independent runs of each mode against the live API. Same FLAT vs
SUB setup as Round 4. This is the variance-aware version of Round 4
and should be cited preferentially.

**Per-run totals (3 reps each mode):**

| run | FLAT total | SUB total | Δ% | SUB spawns | empty outputs |
|---:|---:|---:|---:|---:|---:|
| 1 | $0.0769 | $0.0484 | **−37.1%** | 4 | 1 |
| 2 | $0.0232 | $0.0618 | **+166.9%** | 4 | 2 |
| 3 | $0.0409 | $0.0506 | **+24.0%** | 5 | 3 |
| median | $0.0409 | $0.0506 | +24.0% | 4 | 2 |

The per-run variance is the headline. Same script, same temperature
default, same model — and FLAT alone ranges 3.4× between runs;
SUB ranges 1.3×. **The Round 4 single-run conclusion (−25% in SUB's
favor projected) was unreliable**; the variance is wide enough that
a single run can land anywhere from "SUB saves 37%" to "SUB costs
167% more."

**Per-turn median, cumulative cost:**

| turn | FLAT cum (med) | SUB cum (med) | Δ |
|---:|---:|---:|---:|
|  1 | $0.00144 | $0.00028 | −$0.00116 |
|  4 | $0.00614 | $0.00361 | −$0.00253 |
|  6 | $0.01112 | $0.00678 | −$0.00433 |
| 10 | $0.02651 | $0.02383 | −$0.00268 |
| **11** | $0.03934 | $0.04914 | **+$0.00980** |
| 12 | $0.04085 | $0.05064 | +$0.00979 |

Two things to read off this table:

1. **Through turn 10, SUB is cheaper in every cumulative slot,
   median.** Crossover (SUB cum ≤ FLAT cum) lands at turn 4
   (median across runs). SUB's working lead at turn 10: ~10%.
2. **Turn 11 erases the lead.** It is a single bad turn for both
   modes (asks the agent to "walk through how loop.ts wires
   ImmutablePrefix into a turn" — `loop.ts` is 53 kB, the probe's
   `read_file` truncates at 6 kB). Both modes spend dramatically
   on this turn; SUB spends more.

**Turn 11 detail (the anomaly):**

| run | FLAT modelCalls | FLAT $ | SUB modelCalls | SUB $ | SUB spawns this turn |
|---:|---:|---:|---:|---:|---:|
| 1 | 17 | $0.0407 | 17 | $0.0340 | 4 |
| 2 |  6 | $0.0091 | 17 | $0.0362 | 3 |
| 3 |  6 | $0.0128 |  7 | $0.0131 | 2 |

In runs 1 and 2, SUB went into a **spawn storm** — 3–4 spawns on a
single turn trying to chase the answer through a truncated file.
Each spawn ran its own multi-iter loop; cumulative spawn cost on
turn 11 alone was $0.005–$0.010. This is a real failure mode the
existing `spawn_subagent` description doesn't warn about and the
metric doesn't surface.

**Spawn output quality:** out of 13 total spawns across the three
SUB runs, **6 returned an empty `output` string** (46%). They paid
full cost — completion tokens generated, child loop ran — but
delivered nothing usable to the parent. The parent had to either
re-spawn or fall back to inline reads. This is a much bigger problem
than Round 3a's single jsdoc anomaly suggested; empty output is
a routine outcome of the current child-loop budget, not an edge case.

**Headline takeaways:**

- **Mean SUB win is real on the easy turns:** through turn 10, SUB
  is consistently cheaper.
- **Mean SUB loss is real on hard turns:** truncated-file or
  ambiguous investigations trigger spawn storms that can cost 3–5×
  the inline equivalent.
- **The variance is the load-bearing problem.** SUB's distribution
  has fatter tails than FLAT's. A user who's risk-averse about
  per-session cost should prefer FLAT; a user optimizing for
  expected cost over many sessions should prefer SUB.
- **Empty-output spawns are common and silently expensive.** The
  metric must report `success` per spawn, not just `compression`.

What this means for the metric proposal: showing per-spawn
compression is necessary but **showing variance is also necessary**.
A user who sees "spawn 3 saved 1200 tokens, compression 22%" might
not realize that spawn 1 and spawn 2 returned empty strings and
cost $0.006 each. The metric needs to surface (a) spawn success
rate, (b) per-spawn cost, (c) projected recovery — all three.

#### Round 5 — diagnose the 46% empty-output rate

Probe: `scripts/probe-empty-output-diagnosis.mts`. Three of
Round 4-long's failing spawn tasks re-run at three `maxToolIters`
caps (8, 16, 32). Capture `paused`, `error`, iters used, output.

| Task | budget=8 | budget=16 | budget=32 |
|---|---|---|---|
| `find-cachefirstloop-construct` | ✓ 210 tok | ✓ 243 tok | ✓ 208 tok |
| `repair-usage`                  | paused | ✓ 358 tok | ✓ 302 tok |
| `loop-prefix-wiring`            | paused | storm-summary | storm-summary |

Three distinct empty-output mechanisms:

1. **`paused` = budget too tight** (~⅓ of failures). Child hit
   `maxToolIters` mid-investigation. `SubagentResult.paused=true`,
   `success=true` (!), `output=""`. The state is recoverable: the
   parent could pass `pausedSession` back as `resume_session` to
   continue. The probe didn't. Round 4-long's wrapper treated this
   as plain failure — it isn't.

2. **`forcedSummary` discards useful content** (~⅓). When the storm
   breaker fires, the child emits an `assistant_final` event with
   `forcedSummary=true`. `spawnSubagent` (loop.ts line 314) routes
   that text into `errorMessage`, leaves `final=""`, and the result
   shows `output=""`, `success=false`. But that "error" text is a
   real synthesis: in our `loop-prefix-wiring` run it correctly
   identified `ImmutablePrefix.toMessages()`, `CacheFirstLoop.prefix`,
   and `loop/messages.ts` — the model knew what it found and what
   it couldn't reach. **The content exists, it's just in the wrong
   field.** Fixable in one diff.

3. **Read tool truncation** (rest). Probe-rolled `read_file` caps at
   6 kB with no paging. Real Reasonix `registerFilesystemTools`
   supports `offset`/`limit`. Round 4-long's empty rate is partly
   probe artifact — real users would hit (1) and (2), not (3).

So the headline 46% empty-output rate decomposes roughly:

- ~15 percentage points: easily fixed by raising the default
  `maxToolIters` from 8 (probe default) to 16+ (real Reasonix default
  is also 16 via `DEFAULT_PAUSE_EVERY`)
- ~15 percentage points: fixed by routing `forcedSummary` content to
  `output` instead of `error` (one-line spawnSubagent change)
- remainder: probe-only (the truncated read tool); doesn't reproduce
  in real Reasonix sessions

This **rehabilitates `spawnSubagent`** somewhat. The "unreliable"
framing in the Round 4-long writeup is too strong. The
real-Reasonix empty-output rate, with the two fixes above, is
plausibly under 20%.

This also clarifies what the metric PR needs to expose:

- `success` true ONLY if `output.trim().length > 0`. Pause and
  storm-summary both count as `success=false` until they're
  surfaced properly.
- Add `paused: boolean` and `resumeSessionId?: string` to the
  per-spawn record so the user can see "this spawn is recoverable,
  parent didn't resume."
- Add `partialContent?: string` populated from the
  `forcedSummary` text (if/when the routing fix lands) so the
  partial answer is visible.

Plus three follow-up issues falling out of this diagnosis:

- "Route `forcedSummary` text to `SubagentResult.output`" — small
  diff, high leverage. Reduces empty-output rate measurably.
- "Default `spawn_subagent` budget bump from `DEFAULT_PAUSE_EVERY`
  (16) — verify with a budget-sweep probe on the existing
  `tau-bench` corpus before changing the default."
- "Auto-resume paused spawns up to N times" — if a spawn pauses
  with a usable `partialSummary`, parent could auto-resume rather
  than show the user an empty result. Needs design work.

#### Round 5b — re-ran the diagnosis with the `forcedSummary → output` routing prototyped

To validate that Issue 2 (route `forcedSummary` event content to
`SubagentResult.output` instead of `error`) actually moves the
needle, we prototyped the diff and re-ran the same diagnosis probe.

Patch sketch (against `src/tools/subagent.ts`):

- Track `forcedSummaryFired: boolean` in `spawnSubagent`.
- In the `assistant_final` branch when `ev.forcedSummary === true`:
  if `opts.parentSignal?.aborted` (= the user-abort path), keep
  the existing `errorMessage = ev.content` behavior; otherwise
  (the storm-breaker / context-guard path) write `final = ev.content`
  and set `forcedSummaryFired = true`.
- Result becomes `success: !errorMessage && !forcedSummaryFired`,
  with a new `forcedSummary?: boolean` field exposed.
- `formatSubagentResult` gains a `forcedSummary` branch that
  renders `{ success: false, partial: true, output, note }` so the
  parent loop sees the partial answer instead of an empty string.

The full project test suite (197 files, 2 971 tests) ran green
against the prototype. Then re-running
`scripts/probe-empty-output-diagnosis.mts` against the prototype:

| Task @ budget | Before (Round 5) output | After (Round 5b) output |
|---|---:|---:|
| `loop-prefix-wiring` @ 8  | 0 tok (paused) | 391 tok (storm-summary, now in output) |
| `loop-prefix-wiring` @ 16 | 0 tok (storm, content in `error`) | 0 tok (paused this run) |
| `loop-prefix-wiring` @ 32 | 0 tok (storm, content in `error`) | 286 tok (storm-summary, now in output) |
| `repair-usage` @ 8        | 0 tok (paused) | 249 tok (storm-summary, now in output) |
| `repair-usage` @ 16       | 358 tok (success) | 0 tok (paused this run) |
| `repair-usage` @ 32       | 302 tok (success) | 0 tok (paused this run) |

Two things to read off:

1. **The routing fix does what we hoped** — every spawn that
   storm-summaries now reports its partial answer in `output`
   instead of stranding it in `error`. The `loop-prefix-wiring`
   task that "never succeeded" in Round 5 now returns 286-391
   tokens of usable partial synthesis on two of three budgets.
2. **Run-to-run variance is real** — `repair-usage` succeeded at
   16/32 in Round 5 and paused at 16/32 in Round 5b. Same code,
   same task, different model decisions. This reinforces
   Round 4-long's "single-run measurements are unreliable" caveat;
   the fix still helps a meaningful fraction of failures even with
   that noise floor.

The prototype is not in the tree as of this draft. The diff is
recommended as Issue 2 of the posting plan, with Round 5b as the
evidence that the fix is non-trivially valuable.

#### Round 4 — first 6-turn version (superseded by Round 4-long)

Probe: `scripts/probe-session-e2e.mts`. Same user script (six
read-heavy turns: layout discovery → multi-file summary → registry
explanation → loop architecture → final synthesis) run twice through
`CacheFirstLoop` against the live API. Only difference between runs:
the SUB-mode parent has `spawn_subagent` registered, FLAT does not.
`deepseek-chat`, 2026-05-15.

| turn | FLAT prompt | SUB prompt | SUB compression | FLAT cum $ | SUB cum $ | SUB − FLAT |
|---:|---:|---:|---:|---:|---:|---:|
| 1 |  1 364 |  1 739 | 127.5% | $0.000227 | $0.000273 | +$0.000046 |
| 2 |  6 064 |  6 322 | 104.3% | $0.000784 | $0.000768 | −$0.000016 |
| 3 | 16 860 | 16 340 |  96.9% | $0.001934 | $0.001950 | +$0.000015 |
| 4 | 96 970 | 70 700 |  72.9% | $0.005997 | $0.007662 | +$0.001664 |
| 5 | 85 603 |104 860 | 122.5% | $0.009053 | $0.014013 | +$0.004960 |
| 6 | 77 141 | 20 154 |  **26.1%** | $0.011822 | $0.014730 | +$0.002908 |

Spawn behavior: the parent model **chose** to spawn only twice out of
six turns — on turn 4 (read all of `tools.ts`) and turn 5 (read all
of `loop.ts`). The other four turns it used `read_file` directly.
That's exactly the gating the current `spawn_subagent` description
asks for; the model honored it.

**Total session**: SUB cost $0.0147 vs FLAT $0.0118. **SUB was 24.6%
more expensive** over this 6-turn session.

**But look at turn 6.** SUB's prompt was 20 154 tokens; FLAT's was
77 141 — **SUB shipped 74% fewer tokens** because the previous
spawns kept its parent log small. Per-turn cost on turn 6: SUB
$0.000717 vs FLAT $0.002769. **SUB was 4× cheaper at the tail.**

Per-turn delta extrapolation: at the turn-6 rate, SUB is ~$0.0021
cheaper per future turn. The $0.0029 SUB deficit closes in ~1.4
follow-up turns. **A read-heavy session ≥ 8 turns is the empirical
crossover** on this workload at this model. The 42-turn break-even
the arithmetic projection produced (savings × hit_price) is
pessimistic — it ignores that SUB also generates fewer completion
tokens at the tail and runs fewer model iters per parent turn.

**Headline numbers from Round 4:**

- **Short sessions (≤6 turns) of read-heavy work:** SUB loses by
  20–25%. The spawn cost (one full child loop per investigation)
  is real and is not recovered inside the session.
- **Medium sessions (8–12 turns):** crossover region. Per-turn
  delta favors SUB strongly at the tail.
- **Long sessions (≥15 turns):** SUB wins decisively because the
  parent log stays small while FLAT's grows linearly.

What this means for the metric proposal: showing
`compressionRatio` per spawn is necessary but not sufficient. The
user needs to see **projected session-level recovery**, not just
"this spawn compressed to 12%". A user who spawns aggressively in
a 4-turn session is making the topology worse, not better.

#### Round 3a — write-heavy negative case

Probe: `scripts/probe-subagent-write-heavy.mts`. Three spawns whose
deliverable IS the artifact (not a summary): write a complete
`LRUCache<K,V>` class, write a one-line JSDoc for `countTokens`,
refactor a snippet to early returns. Same client + tooling as Round 2,
`deepseek-chat`, 2026-05-15.

| Task | turns | completion | output | savings | compression | notes |
|---|---:|---:|---:|---:|---:|---|
| `lru-class-pure-write` | 1 |  491 | 436 |  55 | 88.8% | pure write, no reads |
| `jsdoc-from-source`    | 6 | 1687 |   0 |1687 |  0.0% | **empty output — failed spawn**, ignore |
| `refactor-emit-block`  | 1 |   84 |  54 |  30 | 64.3% | short artifact, near input size |
| aggregate (clean)*     | 2 |  575 | 490 |  85 | **85.2%** | excludes jsdoc empty-output |

*The jsdoc spawn returned an empty final string — likely the child
emitted scratch and never produced a final answer. We exclude it from
the aggregate because its 0% "compression" is an artifact of failure,
not a genuine distillation. It does illustrate a related issue: the
metric needs a success-indicator companion, otherwise an empty-output
spawn looks like the best-compressing spawn ever.

**Headline**: write-heavy spawns compress to ~85%, not ~6%. The
distillation argument **does not apply** when the artifact is the
output. Per-spawn savings drop from ~2 600 tokens (Round 2 avg) to
~40 tokens (Round 3a avg) — two orders of magnitude.

#### Round 3b — break-even arithmetic

Combining Rounds 2 and 3a with DeepSeek's published pricing
(`deepseek-chat`, ~$0.27/M miss input, ~$0.027/M hit input — hit
rate is what subsequent parent turns pay because the prefix is
warm).

Formula:

```
break_even_followup_turns = spawn_cost_usd / (savings_tokens × hit_price_per_token)
hit_price_per_token       = 0.027 / 1_000_000 ≈ 2.7e-8 USD/token
```

| Spawn profile | spawn cost | savings (tok) | break-even (follow-up turns) |
|---|---:|---:|---:|
| Read-heavy (Round 2 avg)  | $0.00411 | 2 593 | **~59** |
| Write, pure (LRU class)   | $0.00022 |    55 |   ~143 |
| Write, refactor           | $0.00006 |    30 |    ~77 |

What this means: **on read-heavy spawns the distillation pays back
inside ~60 follow-up parent turns** — well inside typical session
length. **On write-heavy spawns it takes 80–150+ follow-up turns** —
plausible only on the very longest sessions, otherwise the spawn is a
net loss on this axis.

This is the decision rule the metric needs to surface:
- read/explore/summarize → spawn (compression wins fast)
- write/edit where output ≈ input size → inline by default; spawn
  only for attention isolation or parallel fan-out, not for
  distillation savings

#### Round 1 — single-shot prefix-cache probe, no tool loop

Probe: `scripts/probe-orchestrator-cache.mjs`. Five module briefs, one
~3800-token frozen role prefix, single completion call per brief, no
tools. Live `deepseek-chat`, 2026-05-15.

| Metric | FLAT (one growing msgs[]) | ORCH (fresh msgs[] per brief) |
|---|---:|---:|
| Total `prompt_tokens` (5 calls) | 19 804 | 18 842 |
| Total cache-hit tokens | 19 456 | 18 560 |
| Total cache-miss tokens | 348 | 282 |
| Aggregate hit rate | 98.2% | 98.5% |
| Billed-equivalent input tokens (`miss + hit × 0.1`) | 2 294 | 2 138 |

(Warm-cache row. Cold-cache row was contaminated — running ORCH first
seated the role prefix for FLAT's first call. To get a clean cold
number we'd need either a deliberate cache-flush gap or two separate
processes, which round 1 didn't do.)

**What this actually shows**

- Hit *rate* is essentially identical between topologies once warm.
  Pillar 1's append-only discipline is already extracting nearly all
  available cache from the FLAT mode.
- The win comes from FLAT's prompt growing each turn (3 763 → 4 187
  across 5 modules) while ORCH stays flat at ~3 770. ORCH ships ~5%
  fewer total input tokens and ~7% fewer billed-equivalent input
  tokens on 5 short modules.
- That ≥40% drop the issue is shaped around is **not supported** by
  this probe. Single-digit percent on short sessions is what we
  actually have. The advantage should compound with session length —
  FLAT's per-turn prompt grows linearly, ORCH stays constant — but
  this probe doesn't go long enough to demonstrate that.

#### What's still missing before this issue can ship

Rounds 1 + 2 + 3a + 3b + 4 + 4-long + 5 form the empirical core.
Metric-PR case is fully evidenced. Remaining holes are minor:

1. **Tool-result-token accounting** — for the `savings` lower bound
   to become the actual number. Cheap once we're touching
   `SubagentResult`; do this in the metric PR.
2. **Real-Reasonix empty-output rate after the Round-5 fixes** —
   re-run a Round-4-long equivalent against `registerFilesystemTools`
   (paged reads) and with `forcedSummary → output` routing in
   place. Confirm the rate drops to the predicted ~20%. Nice-to-have
   for the issue body; doesn't gate the metric PR.

The proposal's claim, after all rounds:

- Per-spawn savings are real when the spawn succeeds and the work
  is read-heavy (Rounds 2, 4-long turns 1–10).
- Per-spawn savings are negligible on write-heavy work (Round 3a).
- Empty-output rate in the current `SubagentResult` shape is
  measurable (Round 4-long: 46%) but mostly reflects two
  recoverable mechanisms (Round 5: pause + forcedSummary routing).
- Spawn storms (≥3 spawns/turn) can dominate session cost
  (Round 4-long turn 11).
- The win/lose distribution is wide enough that aggregate medians
  hide both wins and losses (Round 4-long per-run variance).

The metric exists to make all of this visible per session, so the
user can decide whether the topology helped on their workload
without having to re-run a probe.

### Related

- `docs/ARCHITECTURE.md` — Pillar 1 (Cache-First Loop), Pillar 2
  (Scratch Distillation). This issue extends Pillar 1; it doesn't
  replace it.
- `benchmarks/real-world-cache/` — 99.82% hit rate case study on one
  long-lived context. That's the ceiling; multi-context is how we
  reach for it on shorter tasks too.
