# Reasonix Architecture

## Design philosophy

Reasonix is **opinionated, not general**. Every abstraction is justified by a
DeepSeek-specific behavior or economic property. If it's generic, we don't
ship it.

The product north star: **coding agent that stays cheap enough to leave on**.
A tool that quietly burns $200/month on a background project is one nobody
uses. Every subsystem below is answerable to that goal.

## The four pillars

### Pillar 1 — Cache-First Loop

**Problem.** DeepSeek bills cached input at ~10% of the miss rate. Automatic
prefix caching activates only when the *exact* byte prefix of the previous
request matches. Most agent loops reorder, rewrite, or inject fresh
timestamps each turn — cache hit rate in practice: <20%.

**Solution.** Partition the context into three regions:

```
┌─────────────────────────────────────────┐
│ IMMUTABLE PREFIX                        │ ← fixed for session
│   system + tool_specs + few_shots        │   cache hit candidate
├─────────────────────────────────────────┤
│ APPEND-ONLY LOG                         │ ← grows monotonically
│   [assistant₁][tool₁][assistant₂]...    │   preserves prefix of prior turns
├─────────────────────────────────────────┤
│ VOLATILE SCRATCH                        │ ← reset each turn
│   R1 thought, transient plan state      │   never sent upstream
└─────────────────────────────────────────┘
```

**Invariants:**
1. Prefix is computed once per session, hashed, and pinned.
2. Log entries are serialized in append order; no rewrites.
3. Scratch is distilled via Pillar 2 before any information from it is folded
   into the log.

**Metric.** `prompt_cache_hit_tokens / (hit + miss)` exposed per-turn and
aggregated per-session. Visible in the TUI's top-bar cache cell.

#### Parallel tool dispatch

Each tool declares `parallelSafe?: boolean` (default `false`). The loop
dispatcher groups consecutive parallel-safe calls into chunks and races
them via `Promise.allSettled`; the first non-parallel-safe call ends the
chunk and runs alone (serial barrier — read-after-write order
preserved). Tool-result yields and history append still land in declared
order regardless of which call settles first, so the model sees the
same shape it would under a fully serial dispatch.

| Env var | Default | Effect |
|---|---|---|
| `REASONIX_PARALLEL_MAX` | `3` (hard cap `16`) | Max chunk size. |
| `REASONIX_TOOL_DISPATCH=serial` | unset | Forces serial dispatch — escape hatch. |

Built-in opt-ins: read-only filesystem (`read_file`, `list_directory`,
`directory_tree`, `search_files`, `search_content`, `get_file_info`),
web (`web_search`, `web_fetch`), `recall_memory`, `semantic_search`,
isolated child loops (`run_skill`, `spawn_subagent`), in-memory job
queries (`job_output`, `list_jobs`). Mutating / side-effecting tools
stay default. MCP-bridged tools default `false` — third-party tools
opt in only when the server explicitly declares parallel safety.

### Pillar 2 — Tool-Call Repair

**Problem.** Empirical DeepSeek failure modes:
- Tool-call JSON emitted inside `<think>`, missing from the final message.
- Arguments dropped when schema has >10 params or deeply nested objects.
- Same tool called repeatedly with identical args (call-storm).
- Truncated JSON due to `max_tokens` hit mid-structure.

**Solution.** Four passes:

1. **`flatten`** — schemas with >10 leaf params or depth >2 are auto-detected
   on `ToolRegistry.register()` and presented to the model in dot-notation
   form. `dispatch()` re-nests the args before calling the user's `fn`.
2. **`scavenge`** — regex + JSON parser sweeps `reasoning_content` for any tool
   call the model forgot to emit in `tool_calls`.
3. **`truncation`** — detect unbalanced JSON and repair by closing braces or
   requesting a continuation completion.
4. **`storm`** — identical `(tool, args)` tuple within a sliding window →
   suppress the call, inject a reflection turn.

### Pillar 3 — Cost Control *(v0.6)*

**Problem.** Coding agents that default to the frontier model (v4-pro, ~12×
flash cost) and accumulate full tool results in context are $150-$250/month
for active users. Most turns don't need frontier reasoning; most sessions
re-pay for tool results that were only useful once.

**Solution.** Four complementary mechanisms, none of which require manual
tuning in the common case:

#### 4.1 Tiered defaults (flash-first)

The three presets trade **model tier** and **reasoning effort**:

| Preset | Model | Effort | Cost |
|---|---|---|---:|
| `flash` | `v4-flash` | `max` | 1× |
| `auto` (default) | `v4-flash` → `v4-pro` on hard turns | `max` | 1–3× |
| `pro` | `v4-pro` | `max` | ~12× |

All auxiliary calls — `forceSummaryAfterIterLimit`, subagent spawns,
truncation repair retries — hard-code `v4-flash + effort=high` regardless
of the user's preset. There's no reason to pay pro rates for "paraphrase
these tool results into prose" or for an `explore` subagent's grep chain.

#### 4.2 Turn-end auto-compaction

Every tool result in the log exceeding `TURN_END_RESULT_CAP_TOKENS` (3000)
is shrunk to that cap when a turn ends. The model had the full text for
the turn that read it; subsequent turns see a compact summary and can
re-read if needed. One extra `read_file` call is vastly cheaper than
dragging 12KB through every future prompt.

A proactive 40% context-ratio threshold runs the same shrink pre-emptively
inside long multi-iter turns before the 80% emergency threshold fires.

#### 4.3 `/pro` single-turn arming

Users who predict a hard task type `/pro`; the **next** turn runs on
`v4-pro`, then auto-disarms. No preset churn, no forgotten revert. Armed
state is visible as a yellow `⇧ pro armed` pill in the header.

#### 4.4 Failure-signal auto-escalation

The loop counts visible "flash is struggling" events per turn:
- `edit_file` / `write_file` SEARCH-not-found errors
- ToolCallRepair fires (scavenge / truncation-fix / storm-break)

Once the count hits `FAILURE_ESCALATION_THRESHOLD` (3), the **remainder of
the current turn** runs on `v4-pro`. Announced via a yellow warning row —
no silent cost surprises. Counter + escalation flag reset at every turn
start.

Header shows a red `⇧ pro escalated` pill while the turn is on pro.

#### Cost transparency

Per-turn and session cost are colored in the StatsPanel:
- `turn $0.003` — green <$0.05, yellow $0.05–0.20, red ≥$0.20
- `session $0.12` — same scale ×10

## Module layout

```
src/
├── client.ts               # DeepSeek client (fetch + SSE)
├── loop.ts                 # Pillar 1 + 3 — CacheFirstLoop
├── repair/                 # Pillar 2 pipeline
│   ├── index.ts
│   ├── scavenge.ts
│   ├── flatten.ts
│   ├── truncation.ts
│   └── storm.ts
├── prompt-fragments.ts     # TUI_FORMATTING_RULES, NEGATIVE_CLAIM_RULE —
│                           #   reused by main + subagent + skill prompts
├── code/prompt.ts          # reasonix code main system prompt
├── tools/                  # Tool implementations
│   ├── filesystem.ts       # read / list / search / edit / write
│   ├── shell.ts            # run_command + run_background (JobRegistry)
│   ├── jobs.ts             # background-process registry
│   ├── memory.ts           # remember / forget / list user memories
│   ├── skills.ts           # list + invoke SKILL.md playbooks
│   ├── subagent.ts         # spawn_subagent — flash+high by default
│   ├── plan.ts             # submit_plan (review gate)
│   └── web.ts              # web_search, web_fetch (multi-engine: Mojeek or SearXNG)
├── mcp/                    # MCP client + bridge (stdio + SSE)
├── memory.ts               # ImmutablePrefix / AppendOnlyLog / VolatileScratch
├── project-memory.ts       # REASONIX.md loader
├── user-memory.ts          # ~/.reasonix/memory/ store (project + global)
├── skills.ts               # built-in explore + research skills
├── session.ts              # JSONL session persistence
├── telemetry.ts            # cost + cache-hit accounting + SessionSummary
├── tokenizer.ts            # DeepSeek V3 tokenizer (ported)
├── usage.ts                # ~/.reasonix/usage.jsonl roll-up
├── types.ts                # ChatMessage, ToolCall, ToolSpec
├── index.ts                # library barrel
└── cli/
    ├── index.ts            # commander entry
    ├── resolve.ts          # config + CLI flag precedence
    ├── commands/           # chat, code, run, stats, sessions, ...
    └── ui/
        ├── App.tsx                  # root Ink component (~1984 LOC, was 2931)
        ├── LiveRows.tsx             # spinner rows (OngoingTool / Status / ...)
        ├── EventLog.tsx             # Historical row rendering
        ├── StatsPanel.tsx           # top bar + cost badges
        ├── PromptInput.tsx          # cursor-aware multi-line input
        ├── PlanConfirm.tsx          # submit_plan review modal
        ├── ShellConfirm.tsx         # run_command approval modal
        ├── EditConfirm.tsx          # per-edit review modal
        ├── markdown.tsx             # Ink-native markdown renderer
        ├── edit-history.ts          # EditHistoryEntry + formatters
        ├── useEditHistory.ts        # /undo, /history, /show state machine
        ├── useCompletionPickers.ts  # slash, @, slash-arg pickers
        ├── useSessionInfo.ts        # balance + models + updates fetch
        ├── useSubagent.ts           # subagent sink wiring
        └── slash/                   # /-command implementation
            ├── types.ts             # SlashContext, SlashResult, ...
            ├── commands.ts          # SLASH_COMMANDS data + parse + suggest
            ├── helpers.ts           # git, memory, token formatters
            ├── dispatch.ts          # registry + handleSlash lookup
            └── handlers/            # per-topic: basic, mcp, memory,
                                     # skill, admin, observability, edits,
                                     # jobs, sessions, model (/pro lives here)
```

Files kept small by design: the largest module under `cli/ui/` is 2K
lines (App.tsx), every handler under `slash/handlers/` is ≤200 lines,
every hook under `cli/ui/` is ≤310 lines. Adding a new slash command
means editing one handler file and one registry line.

## Design evolution

- **v0.0.x** — Pillar 1 end-to-end, repair pipeline complete, Ink TUI scaffold.
- **v0.1** — τ-bench numbers published, streaming polish, transcript replay.
- **v0.3** — MCP client (stdio + SSE), session persistence.
- **v0.4.x** — `reasonix code` with SEARCH/REPLACE edits, review/auto
  gate, background jobs, hooks.
- **v0.5.x** — V4 model support, skills, memory, subagents, actionable
  error messages.
- **v0.6** —
  - **Cost control** (flash-first defaults, auto-compaction, `/pro` one-shot,
    failure-triggered escalation, cost badges).
  - `deepseek-chat` / `deepseek-reasoner` scheduled for deprecation —
    all user-facing surfaces updated to `v4-flash` / `v4-pro`.
  - Shared prompt fragments (`TUI_FORMATTING_RULES`, `NEGATIVE_CLAIM_RULE`).
  - UI refactor: App.tsx split into 6 hooks/components, slash.ts split
    into 13 per-topic modules.
- **v0.31** *(current)* — `branch` + `harvest` features removed entirely
  (the parallel-sample selector and Pillar 2 plan-state extractor); both
  rarely paid for themselves and bloated the slash surface.

## Explicit non-goals

- Multi-agent orchestration as a first-class concept (subagents are a
  cost-reduction mechanism, not a coordination primitive).
- RAG / vector retrieval.
- Support for non-DeepSeek backends (an OpenAI-compatible shim would
  work today via `--model` override, but is not tested).
- Web UI / SaaS.
- Automatic cost escalation without user-visible announcement. Every
  pro-tier model call is surfaced; silent escalation was considered
  and rejected.
