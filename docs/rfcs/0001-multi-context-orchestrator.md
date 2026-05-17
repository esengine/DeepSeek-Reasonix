# RFC-0001 — Multi-context orchestrator

| Field    | Value                                            |
|----------|--------------------------------------------------|
| Status   | Draft                                            |
| Authors  | reasonix                                         |
| Created  | 2026-05-15                                       |
| Target   | Reasonix coding loop                             |
| Pillar   | Pillar 1 (Cache-First Loop) — extension          |

## Summary

Replace the single-context coding loop with an **orchestrator + sub-context**
topology. The orchestrator owns the architecture, decomposition, and final
review. Each sub-context owns exactly one module-sized task and sees only
what the orchestrator hands it. Sub-contexts are short-lived `messages[]`
arrays with role-stable system prompts; the orchestrator splices their
final outputs back into its own log as tool results.

The point is not "agents". The point is that DeepSeek's prefix cache rewards
**short, stable, role-specialized prompts** far more than one giant
everything-prompt. Multi-context is a cache-economics move, not an
agent-framework move.

## Motivation

Today the loop is one `messages[]` per session. Every turn appends to it.
As the session grows we hit three problems:

1. **Input token cost grows linearly per turn**, even at the 10% cached
   rate, because each new turn re-pays the prefix at cache-hit price.
   A 200-turn session on a 40k-token context pays for roughly
   `200 × 40k × 0.1 = 800k` cached input tokens just to keep talking
   to itself.
2. **System prompt drift kills hit rate.** Anything we want to vary per
   sub-task (which tools to expose, which files to focus on, which style
   to enforce) has to either live in the volatile tail (no cache benefit)
   or rewrite the prefix (full miss).
3. **Cross-talk between modules pollutes attention.** When the loop is
   editing module A, having module B's read history in context is pure
   distractor tokens — billed for, and hurting decisions.

A single context is the wrong shape. We want many small contexts, each
with a stable role prefix, each addressing one concern.

## Background — what DeepSeek's cache actually does

This is the load-bearing fact and we should write it down once so future
RFCs can reference it instead of re-deriving it.

- **Granularity:** prefix is hashed in **64-token blocks**. Anything
  shorter than one block does not enter the cache at all.
- **Matching rule:** longest common prefix **from token index 0** with a
  previous request to the same model. A single token change at position
  `i` invalidates the cache from `i` onward — everything after is a miss.
- **No explicit markers.** Unlike Anthropic's `cache_control`, DeepSeek
  caching is automatic. You don't opt in. You only design for it.
- **Pricing:** cache hit input ≈ 10% of cache miss input. Output is
  unaffected. The economic gradient pushes hard toward making the prefix
  long, stable, and reused.

Implication for context design: every `messages[]` array we send is
effectively keyed by its serialized prefix. Two arrays that share a long
identical prefix cost almost nothing to extend. Two arrays whose system
prompts differ by one token share **zero** cache.

## Proposal

### Topology

```
┌──────────────────────────────────────────────────┐
│ ORCHESTRATOR CONTEXT                             │
│   system: "You decompose tasks and dispatch."    │
│   tools:  spawn(role, brief), read_tree, ...     │
│   log:    user request → tool_calls → results    │
└─────────────────┬────────────────────────────────┘
                  │ spawn(role="module-coder", brief=...)
        ┌─────────┼──────────┬───────────────┐
        ▼         ▼          ▼               ▼
   ┌─────────┐ ┌─────────┐ ┌─────────┐  ┌─────────┐
   │ SUB-CTX │ │ SUB-CTX │ │ SUB-CTX │  │ SUB-CTX │
   │ coder A │ │ coder B │ │ reviewer│  │ doc-gen │
   └─────────┘ └─────────┘ └─────────┘  └─────────┘
   each: own messages[], role-stable system, dies after final()
```

The orchestrator never sees a sub-context's intermediate turns. It sees
the brief it sent in, and the single final string the sub-context
returned. That string is appended to the orchestrator log as a synthetic
tool result.

### Wire-level invariants

A "context" is just an in-memory `messages: Message[]`. There is no
server-side state. We commit to these properties:

1. **Each role gets a frozen system prompt.** `architect`, `coder`,
   `reviewer`, `doc-gen` etc. The text is pinned at build time and
   hashed; if the hash changes the role is renamed.
2. **Sub-context messages[] is built from:**
   `[role.system, role.tool_specs, role.fewshots, brief, ...turns]`.
   The first three are the immutable prefix and identical for every
   call to that role.
3. **Brief is the only per-task variable in the prefix region.** It is
   short (target ≤500 tokens) and rendered deterministically — same
   inputs produce byte-identical briefs.
4. **No timestamps, no UUIDs, no `Date.now()` anywhere in the prefix.**
   This is the most common cache-killer in agent frameworks. Ban it at
   the type level if we can.
5. **Final result is a string.** Not a structured object the orchestrator
   has to parse with prompt engineering. The sub-context's last
   assistant message, terminated by a designated stop, is the return
   value.

### Cache hit profile

For role `coder`, the 2nd through Nth invocation share the entire
`[system | tool_specs | fewshots]` prefix verbatim. With briefs sized at
~500 tokens and a 6k-token role prefix, we expect:

| Turn   | Prefix tokens | Hit?      | Billed-as-miss tokens |
|--------|---------------|-----------|------------------------|
| 1      | 6500          | no        | 6500                   |
| 2      | 6500 + brief  | first 6500 yes, brief no | ~500 |
| 3..N   | same          | same      | ~500                   |

Steady-state per-call input cost is ~500 miss tokens + 6500 hit tokens
≈ `500 + 650 = 1150` billable-equivalent tokens, versus 6500+ for a
prefix-drifting single-context loop. **Order-of-magnitude win, not a
percentage win.**

### Orchestrator → sub-context handoff

Two ways to model the dispatch, both viable. We should pick one before
implementation:

**Option A — tool call.** Orchestrator emits a `spawn` tool call.
Runtime intercepts, opens a sub-context, runs it to completion, returns
the final string as the tool result. Orchestrator continues. Familiar
shape, fits existing tool dispatcher (`parallelSafe: true` for
independent sub-contexts → free parallelism via the existing chunking
in Pillar 1).

**Option B — explicit subroutine.** Orchestrator outputs structured
JSON (`{spawn: role, brief: ...}`). Runtime drives the recursion in
host code. More legible, no tool-call abuse, but a second protocol to
maintain.

Lean toward A. Tool dispatch already handles parallel-safe chunks,
serial barriers, and result ordering — sub-contexts are just slower
tool calls.

### What the orchestrator's system prompt says

Approximately (not final wording):

```
You plan and delegate. You do not write code directly.
To make progress, call spawn(role, brief). Briefs must be self-contained;
the sub-context cannot see this conversation.
When all sub-contexts return, integrate their outputs and decide next steps.
```

The "cannot see this conversation" line is doing real work — it forces
the orchestrator to write proper briefs instead of leaning on shared
context the sub-context doesn't actually have.

## Non-goals

- **Not a general agent framework.** We are not building LangGraph.
  Roles are hand-written, finite, and reviewed.
- **Not multi-shot sampling.** No parallel completions, no MCTS-style
  branching. Cheapness is load-bearing; we don't spend tokens to vote.
- **Not memory.** Sub-contexts die after returning. There is no
  long-term per-role memory store in this RFC. If we need it later,
  that's a separate RFC.
- **Not a replacement for Pillar 1.** Orchestrator and each sub-context
  individually still obey the immutable-prefix / append-only-log /
  volatile-scratch partitioning.

## Open questions

1. **Where does the file tree live?** If every sub-context needs
   `read_file`, do they each pay to discover the project structure, or
   does the orchestrator hand them a pre-baked tree slice in the brief?
   Baked slice is cheaper and more deterministic; pick that unless a
   case forces otherwise.
2. **Streaming.** Sub-context streaming is invisible to the user today
   because only the orchestrator's stream reaches the TUI. Do we
   forward sub-context tokens with a role prefix, or just show
   "coder-A working…" spinners? Probably spinners for v1.
3. **Failure mode.** If a sub-context refuses or loops, what does the
   orchestrator see? Proposal: synthetic tool result `{ok: false,
   reason: ...}`, orchestrator decides retry vs. abandon.
4. **Cache prewarming.** Should we issue a tiny dummy call per role at
   session start to seat each role's prefix in DeepSeek's cache before
   the user's real first request lands? Cheap insurance against cold
   cache; needs measurement.
5. **Tool surface per role.** Coder needs `edit_file`. Reviewer should
   not. Orchestrator should not have either. Enforcing this is a
   per-role tool whitelist baked into the role definition. Easy to do,
   easy to forget — must be a typed contract, not a convention.

## Success criteria

If we ship this, the following should be true on a representative
session (e.g. one of the captured tau-bench runs):

- Aggregate `prompt_cache_hit_rate` across orchestrator + all
  sub-contexts ≥ 85% (today's single-context baseline: measure first).
- Total input tokens billed per completed task drops ≥ 40%
  vs. single-context baseline at equal task success rate.
- Task success rate does not regress on the benchmark suite. If it
  drops, the RFC fails and we revert — cheapness is not worth wrong
  answers.

## References

- `docs/ARCHITECTURE.md` — Pillar 1 (Cache-First Loop), Pillar 2
  (Scratch Distillation).
- `benchmarks/real-world-cache/` — 99.82% hit rate case study on a
  single long-running context; shows the ceiling when prefix discipline
  is perfect. Multi-context is how we approach that ceiling on
  short-lived tasks too.
