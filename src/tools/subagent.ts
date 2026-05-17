/** Isolated child loop. Inherits parent registry minus spawn_subagent + submit_plan; no hooks; non-streaming. */

import { type DeepSeekClient, Usage } from "../client.js";
import { CacheFirstLoop } from "../loop.js";
import { applyProjectMemory } from "../memory/project.js";
import { ImmutablePrefix } from "../memory/runtime.js";
import { timestampSuffix } from "../memory/session.js";
import {
  NEGATIVE_CLAIM_RULE,
  TUI_FORMATTING_RULES,
  escalationContract,
} from "../prompt-fragments.js";
import { ToolRegistry } from "../tools.js";
import { SUBAGENT_TYPE_NAMES, getSubagentType } from "./subagent-types.js";

/** Side-channel — subagents run inside a tool-dispatch frame, can't go through parent's `LoopEvent` stream. */
export interface SubagentEvent {
  kind: "start" | "progress" | "end" | "inner" | "phase";
  /** Stable per-spawn id; lets the UI key parallel runs apart instead of overwriting one shared row. */
  runId: string;
  task: string;
  skillName?: string;
  model?: string;
  iter?: number;
  elapsedMs?: number;
  summary?: string;
  error?: string;
  turns?: number;
  costUsd?: number;
  usage?: Usage;
  /** When kind === "inner": the raw child loop event. Parent UI translates to a child summary. */
  inner?: import("../loop.js").LoopEvent;
  /** When kind === "phase": coarse status verb for the activity row. */
  phase?: "exploring" | "summarising";
}

let runIdCounter = 0;
function nextRunId(): string {
  runIdCounter++;
  return `sub-${runIdCounter.toString(36)}`;
}

export interface SubagentSink {
  current: ((ev: SubagentEvent) => void) | null;
}

export interface SpawnSubagentOptions {
  client: DeepSeekClient;
  parentRegistry: ToolRegistry;
  system: string;
  task: string;
  model?: string;
  maxResultChars?: number;
  sink?: SubagentSink;
  /** Forwarded into the child loop so parent Esc cancels nested work. */
  parentSignal?: AbortSignal;
  skillName?: string;
  /** Scopes the child registry to these literal tool names; NEVER_INHERITED still wins. Driven by skill `allowed-tools` frontmatter. */
  allowedTools?: readonly string[];
  /** Continue an earlier session instead of starting fresh — loads the prior messages from disk; `task` is treated as a continuation nudge. */
  resumeSession?: string;
}

export interface SubagentResult {
  success: boolean;
  output: string;
  error?: string;
  turns: number;
  toolIters: number;
  elapsedMs: number;
  costUsd: number;
  model: string;
  skillName?: string;
  /** Zero-filled when no API calls landed so consumers always see a valid shape. */
  usage: Usage;
  /** True when the child terminated via forceSummaryAfterIterLimit (storm-breaker / context-guard) — `output` carries the partial synthesis the model managed to produce; not a full answer. User-abort forced summaries do NOT set this (their content is a UX placeholder, routed to `error`). */
  forcedSummary?: boolean;
}

export interface SubagentToolOptions {
  client: DeepSeekClient;
  defaultSystem?: string;
  projectRoot?: string;
  defaultModel?: string;
  maxResultChars?: number;
  sink?: SubagentSink;
  /** Fires once per spawn, after `spawnSubagent` returns and before its result is formatted for the parent. Bind a `SubagentTelemetry.record` here for automatic distillation capture. */
  onSpawnComplete?: (result: SubagentResult) => void;
}

/** Memory-stable prefix — shared across spawns, cached. The model-dependent escalation contract is appended per spawn so a pro spawn doesn't get told it's running on flash (#582). */
const SUBAGENT_BASE_SYSTEM = `You are a Reasonix subagent. The parent agent spawned you to handle one focused subtask, then return.

Rules:
- Stay on the task you were given. Do not expand scope.
- Use tools as needed. You share the parent's sandbox + safety rules.
- When you're done, your final assistant message is the only thing the parent will see — make it complete and self-contained. No follow-up offers, no questions, no "let me know if you need more."
- Prefer one clear, distilled answer over a long log of what you tried.

${NEGATIVE_CLAIM_RULE}

${TUI_FORMATTING_RULES}`;

function defaultSubagentSystem(modelId: string): string {
  return `${SUBAGENT_BASE_SYSTEM}\n\n${escalationContract(modelId)}`;
}

const DEFAULT_MAX_RESULT_CHARS = 8000;
// Subagents default to flash — their work is read-and-synthesize
// (explore, research), which doesn't need the 12× pro tier. Skill
// frontmatter `model: deepseek-v4-pro` is the opt-in override for
// skills that empirically benefit from the stronger model.
const DEFAULT_SUBAGENT_MODEL = "deepseek-v4-flash";
// Subagents default to effort=high — less thinking budget than a
// main turn (which defaults to `max` in the preset). The parent's
// task arg is already a distilled prompt; explore/research rarely
// need deep chains of thought, and `high` saves output tokens.
const DEFAULT_SUBAGENT_EFFORT: "high" | "max" = "high";

const SUBAGENT_TOOL_NAME = "spawn_subagent";
/** spawn_subagent excluded → depth=1 hard cap; submit_plan excluded → no picker mid-parent-turn. */
const NEVER_INHERITED_TOOLS = new Set<string>([SUBAGENT_TOOL_NAME, "submit_plan"]);

/** Per-session spawn count past which the soft hint fires on every subsequent return. */
const SOFT_HINT_AFTER_SPAWNS = 1;
/** Per-session count past which the strong hint fires (asks the model to justify the next spawn). */
const STRONG_HINT_AFTER_SPAWNS = 4;
/** Per-session cumulative subagent token total past which the strong hint also fires. */
const STRONG_HINT_TOKEN_THRESHOLD = 50_000;

/** null → first spawn of the session, no hint. Pure for testability. */
export function subagentBudgetHint(spawnCount: number, totalTokens: number): string | null {
  if (spawnCount > STRONG_HINT_AFTER_SPAWNS || totalTokens >= STRONG_HINT_TOKEN_THRESHOLD) {
    return `[budget: this session has now spawned ${spawnCount} subagents totalling ${totalTokens} tokens. Each spawn pays a fresh prefix-cache miss plus a full child loop — confirm the next spawn is genuinely needed (parallel fan-out or >10-read context blow-up) before calling spawn_subagent again. If you can answer with direct tools, do that instead.]`;
  }
  if (spawnCount > SOFT_HINT_AFTER_SPAWNS) {
    return `[note: this session has spawned ${spawnCount} subagents totalling ${totalTokens} tokens; confirm this one is worth it.]`;
  }
  return null;
}

/** Errors captured in the result shape, never thrown — caller decides how to surface. */
export async function spawnSubagent(opts: SpawnSubagentOptions): Promise<SubagentResult> {
  const model = opts.model ?? DEFAULT_SUBAGENT_MODEL;
  const maxResultChars = opts.maxResultChars ?? DEFAULT_MAX_RESULT_CHARS;
  const sink = opts.sink;
  const skillName = opts.skillName;
  const runId = nextRunId();
  const sessionName = opts.resumeSession ?? `subagent-${runId}-${timestampSuffix()}`;

  const startedAt = Date.now();
  const taskPreview = opts.task.length > 30 ? `${opts.task.slice(0, 30)}…` : opts.task;
  sink?.current?.({
    kind: "start",
    runId,
    task: taskPreview,
    skillName,
    model,
    iter: 0,
    elapsedMs: 0,
  });

  if (opts.allowedTools) {
    const missing = opts.allowedTools.filter((n) => !opts.parentRegistry.has(n));
    if (missing.length > 0) {
      const errorMessage = `subagent allow-list names tool(s) not registered in the parent: ${missing.join(", ")}. Fix the skill's \`allowed-tools\` frontmatter or check spelling.`;
      sink?.current?.({
        kind: "end",
        runId,
        task: taskPreview,
        skillName,
        model,
        iter: 0,
        elapsedMs: Date.now() - startedAt,
        error: errorMessage,
        turns: 0,
        costUsd: 0,
        usage: new Usage(),
      });
      return {
        success: false,
        output: "",
        error: errorMessage,
        turns: 0,
        toolIters: 0,
        elapsedMs: Date.now() - startedAt,
        costUsd: 0,
        model,
        skillName,
        usage: new Usage(),
      };
    }
  }

  const childTools = opts.allowedTools
    ? forkRegistryWithAllowList(
        opts.parentRegistry,
        new Set(opts.allowedTools),
        NEVER_INHERITED_TOOLS,
      )
    : forkRegistryExcluding(opts.parentRegistry, NEVER_INHERITED_TOOLS);
  const childPrefix = new ImmutablePrefix({
    system: opts.system,
    toolSpecs: childTools.specs(),
  });
  const childLoop = new CacheFirstLoop({
    client: opts.client,
    prefix: childPrefix,
    tools: childTools,
    model,
    // Subagents run on a constrained thinking budget by default — the
    // task is already narrow by construction, and `high` cuts output
    // tokens substantially vs `max`.
    reasoningEffort: DEFAULT_SUBAGENT_EFFORT,
    hooks: [],
    stream: true,
    session: sessionName,
  });

  // Wire parent-abort → child-abort. Two pitfalls we have to handle:
  //
  //   1. `addEventListener("abort", ...)` does NOT fire for a signal
  //      that's already aborted (the abort event has already been
  //      dispatched once and `once: true` is moot). If the parent
  //      aborted between dispatch entry and our listener attach,
  //      the listener stays silent forever and the child runs free.
  //      → Check `.aborted` synchronously and forward immediately.
  //
  //   2. childLoop.step() reassigns its internal _turnAbort at the
  //      top of step(). loop.ts forwards prior aborted state into
  //      the fresh controller, so abort() called BEFORE step() runs
  //      still kills the new step at iter 0.
  const onParentAbort = () => childLoop.abort();
  if (opts.parentSignal?.aborted) {
    childLoop.abort();
  } else {
    opts.parentSignal?.addEventListener("abort", onParentAbort, { once: true });
  }

  let final = "";
  let errorMessage: string | undefined;
  let toolIter = 0;
  let summarisingEmitted = false;
  let forcedSummaryFired = false;
  try {
    for await (const ev of childLoop.step(opts.task)) {
      sink?.current?.({ kind: "inner", runId, task: taskPreview, skillName, model, inner: ev });

      if (ev.role === "tool") {
        toolIter++;
        // New tool dispatched — the model went back to deciding, summarising flag resets so the next final-answer delta re-emits.
        summarisingEmitted = false;
        sink?.current?.({
          kind: "progress",
          runId,
          task: taskPreview,
          skillName,
          model,
          iter: toolIter,
          elapsedMs: Date.now() - startedAt,
        });
      }
      // First content delta (no concurrent tool_call_delta role) = the
      // model is now writing its final answer, not deciding the next tool.
      if (ev.role === "assistant_delta" && !summarisingEmitted && (ev.content ?? "").length > 0) {
        summarisingEmitted = true;
        sink?.current?.({
          kind: "phase",
          runId,
          task: taskPreview,
          skillName,
          model,
          phase: "summarising",
          iter: toolIter,
          elapsedMs: Date.now() - startedAt,
        });
      }
      if (ev.role === "assistant_final") {
        if (ev.forcedSummary) {
          // Two paths emit forcedSummary: user-abort (loop.ts ~670) carries a
          // UX placeholder ("aborted by user (Esc)…") that's useless to a
          // parent loop; storm-breaker / context-guard (force-summary.ts)
          // carries a real partial synthesis worth keeping. Discriminate on
          // parentSignal.aborted because the abort path only fires when the
          // signal is set.
          if (opts.parentSignal?.aborted) {
            errorMessage = ev.content?.trim() || "subagent aborted before producing an answer";
          } else {
            final = ev.content ?? "";
            forcedSummaryFired = true;
          }
        } else {
          final = ev.content ?? "";
        }
      }
      if (ev.role === "error") {
        errorMessage = ev.error ?? "subagent error";
      }
    }
  } catch (err) {
    errorMessage = (err as Error).message;
  } finally {
    opts.parentSignal?.removeEventListener("abort", onParentAbort);
  }
  // The loop yields `done` without an `error` event when its API call
  // is aborted mid-flight (intentional UX — see the matching catch in
  // CacheFirstLoop.step). From a SUBAGENT consumer's perspective that
  // still counts as a failure: no answer came back, the parent has
  // nothing to render. Synthesize an error so `success: false` and the
  // UI surfaces the abort instead of returning empty output.
  if (!errorMessage && !final) {
    errorMessage = opts.parentSignal?.aborted
      ? "subagent aborted before producing an answer"
      : "subagent ended without producing an answer";
  }

  const elapsedMs = Date.now() - startedAt;
  const turns = childLoop.stats.turns.length;
  const costUsd = childLoop.stats.totalCost;
  const usage = aggregateChildUsage(childLoop);

  const truncated =
    final.length > maxResultChars
      ? `${final.slice(0, maxResultChars)}\n\n[…truncated ${final.length - maxResultChars} chars; ask the subagent for a tighter summary if you need more.]`
      : final;

  sink?.current?.({
    kind: "end",
    runId,
    task: taskPreview,
    skillName,
    model,
    iter: toolIter,
    elapsedMs,
    summary: errorMessage ? undefined : truncated.slice(0, 120),
    error: errorMessage,
    turns,
    costUsd,
    usage,
  });

  return {
    success: !errorMessage && !forcedSummaryFired,
    output: errorMessage ? "" : truncated,
    error: errorMessage,
    turns,
    toolIters: toolIter,
    elapsedMs,
    costUsd,
    model,
    skillName,
    usage,
    forcedSummary: forcedSummaryFired || undefined,
  };
}

/** Zero-filled when no API calls landed so downstream consumers always see a valid shape. */
function aggregateChildUsage(loop: CacheFirstLoop): Usage {
  const agg = new Usage();
  for (const t of loop.stats.turns) {
    agg.promptTokens += t.usage.promptTokens;
    agg.completionTokens += t.usage.completionTokens;
    agg.totalTokens += t.usage.totalTokens;
    agg.promptCacheHitTokens += t.usage.promptCacheHitTokens;
    agg.promptCacheMissTokens += t.usage.promptCacheMissTokens;
  }
  return agg;
}

export function formatSubagentResult(r: SubagentResult): string {
  if (r.forcedSummary) {
    return JSON.stringify({
      success: false,
      partial: true,
      output: r.output,
      turns: r.turns,
      tool_iters: r.toolIters,
      elapsed_ms: r.elapsedMs,
      cost_usd: r.costUsd,
      note: "Subagent was force-summarized (storm-breaker or context-guard fired). `output` carries the partial synthesis the model produced before being stopped — useful but not a complete answer. Decide whether to accept the partial, narrow the task and re-spawn, or fall back to direct tools.",
    });
  }
  if (!r.success) {
    return JSON.stringify({
      success: false,
      error: r.error ?? "unknown subagent error",
      turns: r.turns,
      tool_iters: r.toolIters,
      elapsed_ms: r.elapsedMs,
    });
  }
  return JSON.stringify({
    success: true,
    output: r.output,
    turns: r.turns,
    tool_iters: r.toolIters,
    elapsed_ms: r.elapsedMs,
    cost_usd: r.costUsd,
  });
}

/** Library surface only — `reasonix code` uses Skills `runAs: subagent` as the user-facing path. */
export function registerSubagentTool(
  parentRegistry: ToolRegistry,
  opts: SubagentToolOptions,
): ToolRegistry {
  const baseSystem = opts.defaultSystem ?? SUBAGENT_BASE_SYSTEM;
  // Bake project memory into the default once — re-reading on every
  // spawn would (a) make the child prefix unstable when REASONIX.md
  // changes mid-session, defeating cache reuse across multiple
  // subagent calls, and (b) cost a stat() per call. The parent itself
  // also reads memory once at startup; matching that semantics keeps
  // subagent and parent on the same page. The escalation contract is
  // appended per-spawn against the spawn's resolved model id (#582).
  const defaultSystemBase = opts.projectRoot
    ? applyProjectMemory(baseSystem, opts.projectRoot)
    : baseSystem;
  const defaultModel = opts.defaultModel ?? DEFAULT_SUBAGENT_MODEL;
  const maxResultChars = opts.maxResultChars ?? DEFAULT_MAX_RESULT_CHARS;
  const sink = opts.sink;
  // Per-session counters survive across spawn calls because registerSubagentTool
  // runs once per parent registry — closure scope is the session scope.
  let sessionSpawnCount = 0;
  let sessionSpawnTokens = 0;

  parentRegistry.register({
    name: SUBAGENT_TOOL_NAME,
    parallelSafe: true,
    description:
      "Spawn an isolated subagent to handle a self-contained subtask in a fresh context, returning only its final answer. **Prefer direct tools.** Spawn primarily for parallel fan-out (2+ independent investigations issued in one tool batch) or when the work would otherwise need >10 file reads/searches whose trail you don't need to keep. Single greps, 1-3 file cross-references, and 'keep my context clean for one question' are NOT good reasons to spawn — direct tools are cheaper and let you reference the evidence later. Each fresh spawn pays a prefix-cache miss plus a full child loop. The subagent inherits your tools but runs in its own isolated message log; only the final assistant message comes back. The subagent runs to completion — same stops as top-level chat (token-context guard, storm breaker, parent Esc cascade).",
    parameters: {
      type: "object",
      properties: {
        task: {
          type: "string",
          description:
            'The subtask the subagent should perform. Be specific and self-contained — the subagent has none of your conversation context, only what you write here. When resuming via `resume_session`, this becomes a continuation nudge (e.g. "finish what you started" or a delta instruction).',
        },
        system: {
          type: "string",
          description:
            "Optional override for the subagent's system prompt. The default tells it to stay focused and return a concise answer; override only when the subtask needs a specialized persona. Ignored on resume — the prior session keeps its original system prompt for cache stability.",
        },
        model: {
          type: "string",
          enum: ["deepseek-v4-flash", "deepseek-v4-pro"],
          description:
            "Which DeepSeek model the subagent runs on. Default is 'deepseek-v4-flash' — cheap and fast, fine for explore/research-style subtasks. Override to 'deepseek-v4-pro' (~12× more expensive) when the subtask genuinely needs the stronger model: cross-file architecture, subtle bug hunts, anything where flash has empirically underperformed.",
        },
        resume_session: {
          type: "string",
          description:
            "Provide a previous subagent's session name to continue it. When set, prior messages are loaded from disk and the original system prompt is reused (cache-friendly). `task` becomes a continuation nudge.",
        },
        type: {
          type: "string",
          enum: [...SUBAGENT_TYPE_NAMES],
          description:
            "Optional persona shaping the system prompt. 'explore' = wide-net read-only investigation, returns a distilled answer. 'verify' = narrow yes/no check with evidence. Omit when supplying your own 'system' or when the default generic persona fits.",
        },
      },
      required: ["task"],
    },
    fn: async (
      args: {
        task?: unknown;
        system?: unknown;
        model?: unknown;
        type?: unknown;
        resume_session?: unknown;
      },
      ctx,
    ) => {
      const task = typeof args.task === "string" ? args.task.trim() : "";
      if (!task) {
        return JSON.stringify({
          error: "spawn_subagent requires a non-empty 'task' argument.",
        });
      }
      const typeSpec = getSubagentType(args.type);
      const model =
        typeof args.model === "string" && args.model.startsWith("deepseek-")
          ? args.model
          : defaultModel;
      const system =
        typeof args.system === "string" && args.system.trim().length > 0
          ? args.system.trim()
          : (typeSpec?.system ?? `${defaultSystemBase}\n\n${escalationContract(model)}`);
      const resumeSession =
        typeof args.resume_session === "string" && args.resume_session.trim().length > 0
          ? args.resume_session.trim()
          : undefined;
      const result = await spawnSubagent({
        client: opts.client,
        parentRegistry,
        system,
        task,
        model,
        maxResultChars,
        sink,
        parentSignal: ctx?.signal,
        resumeSession,
      });
      sessionSpawnCount++;
      sessionSpawnTokens += result.usage.totalTokens;
      if (opts.onSpawnComplete) {
        try {
          opts.onSpawnComplete(result);
        } catch {
          // Telemetry callback errors must not break the spawn-tool dispatch.
        }
      }
      const formatted = formatSubagentResult(result);
      const hint = subagentBudgetHint(sessionSpawnCount, sessionSpawnTokens);
      return hint ? `${formatted}\n${hint}` : formatted;
    },
  });

  return parentRegistry;
}

/** Plan-mode state propagates — a subagent spawned under `/plan` MUST NOT escape it. */
export function forkRegistryExcluding(
  parent: ToolRegistry,
  exclude: ReadonlySet<string>,
): ToolRegistry {
  const child = new ToolRegistry();
  for (const spec of parent.specs()) {
    const name = spec.function.name;
    if (exclude.has(name)) continue;
    const def = parent.get(name);
    if (!def) continue;
    // Re-register copies the public ToolDefinition fields. The child
    // re-runs auto-flatten analysis on its own, which produces an
    // identical flatSchema for the same input — no surprise.
    child.register(def);
  }
  if (parent.planMode) child.setPlanMode(true);
  return child;
}

/** alsoExclude wins over allow so NEVER_INHERITED still drops `spawn_subagent` even if a skill allow-list names it. */
export function forkRegistryWithAllowList(
  parent: ToolRegistry,
  allow: ReadonlySet<string>,
  alsoExclude: ReadonlySet<string>,
): ToolRegistry {
  const child = new ToolRegistry();
  for (const spec of parent.specs()) {
    const name = spec.function.name;
    if (!allow.has(name)) continue;
    if (alsoExclude.has(name)) continue;
    const def = parent.get(name);
    if (!def) continue;
    child.register(def);
  }
  if (parent.planMode) child.setPlanMode(true);
  return child;
}
