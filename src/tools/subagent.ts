/** Isolated child loop. Inherits parent registry minus spawn_subagent + submit_plan; no hooks; non-streaming. */

import { type DeepSeekClient, Usage } from "../client.js";
import { CacheFirstLoop } from "../loop.js";
import { applyProjectMemory } from "../memory/project.js";
import { ImmutablePrefix } from "../memory/runtime.js";
import {
  ESCALATION_CONTRACT,
  NEGATIVE_CLAIM_RULE,
  TUI_FORMATTING_RULES,
} from "../prompt-fragments.js";
import { ToolRegistry } from "../tools.js";

/** Side-channel — subagents run inside a tool-dispatch frame, can't go through parent's `LoopEvent` stream. */
export interface SubagentEvent {
  kind: "start" | "progress" | "end" | "inner" | "phase";
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

export interface SubagentSink {
  current: ((ev: SubagentEvent) => void) | null;
}

export interface SpawnSubagentOptions {
  client: DeepSeekClient;
  parentRegistry: ToolRegistry;
  system: string;
  task: string;
  model?: string;
  maxToolIters?: number;
  maxResultChars?: number;
  sink?: SubagentSink;
  /** Forwarded into the child loop so parent Esc cancels nested work. */
  parentSignal?: AbortSignal;
  skillName?: string;
  /** Scopes the child registry to these literal tool names; NEVER_INHERITED still wins. Driven by skill `allowed-tools` frontmatter. */
  allowedTools?: readonly string[];
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
}

export interface SubagentToolOptions {
  client: DeepSeekClient;
  defaultSystem?: string;
  projectRoot?: string;
  defaultModel?: string;
  maxToolIters?: number;
  maxResultChars?: number;
  sink?: SubagentSink;
}

const DEFAULT_SUBAGENT_SYSTEM = `You are a Reasonix subagent. The parent agent spawned you to handle one focused subtask, then return.

Rules:
- Stay on the task you were given. Do not expand scope.
- Use tools as needed. You share the parent's sandbox + safety rules.
- When you're done, your final assistant message is the only thing the parent will see — make it complete and self-contained. No follow-up offers, no questions, no "let me know if you need more."
- Prefer one clear, distilled answer over a long log of what you tried.

${NEGATIVE_CLAIM_RULE}

${ESCALATION_CONTRACT}

${TUI_FORMATTING_RULES}`;

const DEFAULT_MAX_RESULT_CHARS = 8000;
const DEFAULT_MAX_ITERS = 16;
const MIN_MAX_ITERS = 1;
const MAX_MAX_ITERS = 32;
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

/** Errors captured in the result shape, never thrown — caller decides how to surface. */
export async function spawnSubagent(opts: SpawnSubagentOptions): Promise<SubagentResult> {
  const model = opts.model ?? DEFAULT_SUBAGENT_MODEL;
  const maxToolIters = opts.maxToolIters ?? DEFAULT_MAX_ITERS;
  const maxResultChars = opts.maxResultChars ?? DEFAULT_MAX_RESULT_CHARS;
  const sink = opts.sink;
  const skillName = opts.skillName;

  const startedAt = Date.now();
  const taskPreview = opts.task.length > 30 ? `${opts.task.slice(0, 30)}…` : opts.task;
  sink?.current?.({
    kind: "start",
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
    maxToolIters,
    hooks: [],
    // Streaming on so the parent UI can flip the "summarising" phase the
    // moment the model starts emitting the final answer (first assistant_delta
    // after the last tool result, before assistant_final lands).
    stream: true,
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
  try {
    for await (const ev of childLoop.step(opts.task)) {
      sink?.current?.({ kind: "inner", task: taskPreview, skillName, model, inner: ev });

      if (ev.role === "tool") {
        toolIter++;
        // New tool dispatched — the model went back to deciding, summarising flag resets so the next final-answer delta re-emits.
        summarisingEmitted = false;
        sink?.current?.({
          kind: "progress",
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
          errorMessage = ev.content?.trim() || "subagent ended without producing an answer";
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
    success: !errorMessage,
    output: errorMessage ? "" : truncated,
    error: errorMessage,
    turns,
    toolIters: toolIter,
    elapsedMs,
    costUsd,
    model,
    skillName,
    usage,
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
  const baseSystem = opts.defaultSystem ?? DEFAULT_SUBAGENT_SYSTEM;
  // Bake project memory into the default once — re-reading on every
  // spawn would (a) make the child prefix unstable when REASONIX.md
  // changes mid-session, defeating cache reuse across multiple
  // subagent calls, and (b) cost a stat() per call. The parent itself
  // also reads memory once at startup; matching that semantics keeps
  // subagent and parent on the same page.
  const defaultSystem = opts.projectRoot
    ? applyProjectMemory(baseSystem, opts.projectRoot)
    : baseSystem;
  const defaultModel = opts.defaultModel ?? DEFAULT_SUBAGENT_MODEL;
  const maxToolIters = opts.maxToolIters ?? DEFAULT_MAX_ITERS;
  const maxResultChars = opts.maxResultChars ?? DEFAULT_MAX_RESULT_CHARS;
  const sink = opts.sink;

  parentRegistry.register({
    name: SUBAGENT_TOOL_NAME,
    description:
      "Spawn an isolated subagent to handle a self-contained subtask in a fresh context, returning only its final answer. Use for: deep codebase exploration that would flood the main context, multi-step research where you only need the conclusion, or any focused subtask whose intermediate reasoning the user does not need to see. The subagent inherits all your tools (filesystem, shell, web, MCP, etc.) but runs in its own isolated message log — its tool calls and reasoning never enter your context. Only the final assistant message comes back as this tool's result. Keep tasks focused; the subagent has a stricter iter budget than you do.",
    parameters: {
      type: "object",
      properties: {
        task: {
          type: "string",
          description:
            "The subtask the subagent should perform. Be specific and self-contained — the subagent has none of your conversation context, only what you write here.",
        },
        system: {
          type: "string",
          description:
            "Optional override for the subagent's system prompt. The default tells it to stay focused and return a concise answer; override only when the subtask needs a specialized persona.",
        },
        model: {
          type: "string",
          enum: ["deepseek-v4-flash", "deepseek-v4-pro"],
          description:
            "Which DeepSeek model the subagent runs on. Default is 'deepseek-v4-flash' — cheap and fast, fine for explore/research-style subtasks. Override to 'deepseek-v4-pro' (~12× more expensive) when the subtask genuinely needs the stronger model: cross-file architecture, subtle bug hunts, anything where flash has empirically underperformed.",
        },
        max_iters: {
          type: "integer",
          minimum: MIN_MAX_ITERS,
          maximum: MAX_MAX_ITERS,
          description: `Cap on the subagent's tool-call iterations. Default 16. Lower (e.g. 6-8) for narrow verify-style tasks; higher (e.g. 24) for broad exploration. Hard range: ${MIN_MAX_ITERS}-${MAX_MAX_ITERS}; out-of-range values are clamped to the nearest end.`,
        },
      },
      required: ["task"],
    },
    fn: async (
      args: { task?: unknown; system?: unknown; model?: unknown; max_iters?: unknown },
      ctx,
    ) => {
      const task = typeof args.task === "string" ? args.task.trim() : "";
      if (!task) {
        return JSON.stringify({
          error: "spawn_subagent requires a non-empty 'task' argument.",
        });
      }
      const system =
        typeof args.system === "string" && args.system.trim().length > 0
          ? args.system.trim()
          : defaultSystem;
      const model =
        typeof args.model === "string" && args.model.startsWith("deepseek-")
          ? args.model
          : defaultModel;
      const callerIters = clampMaxIters(args.max_iters);
      const result = await spawnSubagent({
        client: opts.client,
        parentRegistry,
        system,
        task,
        model,
        maxToolIters: callerIters ?? maxToolIters,
        maxResultChars,
        sink,
        parentSignal: ctx?.signal,
      });
      return formatSubagentResult(result);
    },
  });

  return parentRegistry;
}

/** Floats round down; non-finite / wrong-type yields undefined so caller falls back to its default. */
function clampMaxIters(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !Number.isFinite(raw)) return undefined;
  const n = Math.floor(raw);
  if (n < MIN_MAX_ITERS) return MIN_MAX_ITERS;
  if (n > MAX_MAX_ITERS) return MAX_MAX_ITERS;
  return n;
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
