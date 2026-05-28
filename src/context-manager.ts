import { COMPACTION_SUMMARY_MARKER } from "@reasonix/core-utils";
import type { DeepSeekClient } from "./client.js";
import { Usage } from "./client.js";
import { healLoadedMessages } from "./loop.js";
import { stripHallucinatedToolMarkup } from "./loop.js";
import { buildAssistantMessage } from "./loop/messages.js";
import { DEFAULT_MAX_RESULT_CHARS } from "./mcp/registry.js";
import type { AppendOnlyLog } from "./memory/runtime.js";
import { rewriteSession } from "./memory/session.js";
import { type SessionStats, resolveContextTokens } from "./telemetry/stats.js";
import { countTokensBounded, estimateRequestTokens } from "./tokenizer.js";
import type { ChatMessage, ToolSpec } from "./types.js";

function extractPinnedConstraints(systemPrompt: string): string {
  // matchAll because the system prompt can carry multiple blocks under the same
  // prefix — e.g. global User memory + per-project User memory, or several
  // Project memory files. Single .match() would only grab the first.
  const pattern =
    /# (?:HIGH PRIORITY constraints|User memory|Project memory)[\s\S]*?(?=\n# |\n---|$)/g;
  return Array.from(systemPrompt.matchAll(pattern), (m) => m[0]).join("\n\n");
}

/** Auto-fold when a turn's response shows promptTokens above this fraction of ctxMax. */
export const HISTORY_FOLD_THRESHOLD = 0.75;
/** Tail budget after a normal fold, as a fraction of ctxMax. */
export const HISTORY_FOLD_TAIL_FRACTION = 0.2;
/** Above this fraction the normal fold's tail budget didn't buy enough headroom — fold harder. */
export const HISTORY_FOLD_AGGRESSIVE_THRESHOLD = 0.78;
/** Tail budget after an aggressive fold — half the normal one, sacrifices recent context for headroom. */
export const HISTORY_FOLD_AGGRESSIVE_TAIL_FRACTION = 0.1;
/** Skip the fold if the head wouldn't shrink the log by at least this fraction. */
export const HISTORY_FOLD_MIN_SAVINGS_FRACTION = 0.3;
/** Above this fraction we exit the turn with a summary instead of folding (defense in depth). */
export const FORCE_SUMMARY_THRESHOLD = 0.8;
/** Turn-start local estimate above this fraction triggers a pre-iter fold. Covers cases the
 * post-response fold can't (terminal prior turn, fresh session restore, huge user paste). */
export const TURN_START_FOLD_THRESHOLD = 0.9;
/** Hard deadline for semantic fold summaries so a hung request cannot stall the turn loop. */
export const HISTORY_FOLD_SUMMARY_TIMEOUT_MS = 15_000;
/** Prepended to fold summary content so the model knows it's a synthesized recap.
 *  Re-export of the shared constant so existing imports keep resolving. */
export const HISTORY_FOLD_MARKER = COMPACTION_SUMMARY_MARKER;
/** Header that precedes preserved skill bodies in a fold's synthesized assistant message. */
export const SKILL_PIN_MEMO_HEADER = "[Active skill memos — preserved verbatim across the fold:]";
/** Matches the wrapper emitted by `run_skill` so the fold can lift bodies out before summarizing. */
const SKILL_PIN_REGEX = /<skill-pin name="([^"]+)">\n[\s\S]*?\n<\/skill-pin>/g;

export interface ContextManagerDeps {
  client: DeepSeekClient;
  log: AppendOnlyLog;
  stats: SessionStats;
  sessionName: string | null;
  getAbortSignal: () => AbortSignal;
  getCurrentTurn: () => number;
  getSystemPrompt: () => string;
  /** Reuses the live prefix → fold summary call shares the cached bytes the main agent already paid for. */
  getToolSpecs?: () => readonly ToolSpec[];
  getFewShots?: () => readonly ChatMessage[];
  /** Fired when the message log was rewritten by fold; lets the loop drop session-scoped caches whose validity rested on the elided history (e.g. read-before-edit tracker). */
  onLogRewrite?: () => void;
}

export type PostUsageDecisionKind = "none" | "fold" | "exit-with-summary";

export interface PostUsageDecision {
  kind: PostUsageDecisionKind;
  promptTokens: number;
  ctxMax: number;
  ratio: number;
  /** Token budget for the recent tail when kind === "fold"; smaller in the aggressive band. */
  tailBudget?: number;
  /** True when this fold is in the 70-85% band — used in user-facing messaging. */
  aggressive?: boolean;
}

export interface FoldResult {
  folded: boolean;
  beforeMessages: number;
  afterMessages: number;
  summaryChars: number;
}

function buildFoldSummaryInstruction(pinnedSkillNames: string[]): string {
  const base =
    "Summarize the conversation above as one self-contained prose recap. Preserve the user's " +
    "ORIGINAL OBJECTIVE (never paraphrase away negative constraints like 'do NOT do X'), all " +
    "'do not' / 'never' / 'avoid' instructions, decisions reached, files inspected or modified, " +
    "tool results still relevant, and any open todos. Skip turn-by-turn play-by-play. " +
    "Output plain prose only — no tool calls, no markdown headings, no SEARCH/REPLACE blocks.";
  if (pinnedSkillNames.length === 0) return base;
  const list = pinnedSkillNames.map((n) => `"${n}"`).join(", ");
  return `${base} The following skill memos are pinned verbatim and appended after your summary — do NOT quote or paraphrase their bodies: ${list}.`;
}

// Dedupe by name, last invocation wins. Read-only — leaves head bytes unchanged so the
// summarizer call's prefix still matches what the main agent already cached.
function collectPinnedSkills(head: ChatMessage[]): { names: string[]; bodies: string[] } {
  const pinned = new Map<string, string>();
  for (const msg of head) {
    if (typeof msg.content !== "string") continue;
    SKILL_PIN_REGEX.lastIndex = 0;
    for (const match of msg.content.matchAll(SKILL_PIN_REGEX)) {
      const name = match[1] as string;
      const full = match[0];
      pinned.delete(name);
      pinned.set(name, full);
    }
  }
  return { names: [...pinned.keys()], bodies: [...pinned.values()] };
}

export class ContextManager {
  constructor(private deps: ContextManagerDeps) {}

  /** Real-time token count of the current log — used by Desktop to refresh the
   *  context meter after /compact when no API usage event is available. */
  getLogTokens(): number {
    const entries = this.deps.log.toFullHistory();
    let total = 0;
    for (const e of entries) {
      const content = typeof e.content === "string" ? e.content : "";
      total += countTokensBounded(content);
      if (e.role === "assistant" && Array.isArray(e.tool_calls) && e.tool_calls.length > 0) {
        total += countTokensBounded(JSON.stringify(e.tool_calls));
      }
    }
    return total;
  }

  /** Decision after a turn's response — fold, exit with summary, or carry on. */
  decideAfterUsage(
    usage: Usage | null,
    model: string,
    alreadyFoldedThisTurn: boolean,
  ): PostUsageDecision {
    const ctxMax = resolveContextTokens(model);
    if (!usage) return { kind: "none", promptTokens: 0, ctxMax, ratio: 0 };
    const ratio = usage.promptTokens / ctxMax;
    const base = { promptTokens: usage.promptTokens, ctxMax, ratio };
    if (ratio > FORCE_SUMMARY_THRESHOLD) {
      return { kind: "exit-with-summary", ...base };
    }
    if (alreadyFoldedThisTurn) return { kind: "none", ...base };
    if (ratio > HISTORY_FOLD_AGGRESSIVE_THRESHOLD) {
      return {
        kind: "fold",
        ...base,
        tailBudget: Math.floor(ctxMax * HISTORY_FOLD_AGGRESSIVE_TAIL_FRACTION),
        aggressive: true,
      };
    }
    if (ratio > HISTORY_FOLD_THRESHOLD) {
      return {
        kind: "fold",
        ...base,
        tailBudget: Math.floor(ctxMax * HISTORY_FOLD_TAIL_FRACTION),
        aggressive: false,
      };
    }
    return { kind: "none", ...base };
  }

  /** Turn-start estimate vs ctxMax — caller folds if the ratio crosses
   *  TURN_START_FOLD_THRESHOLD. Replaces the old preflight/mechanical pair. */
  estimateTurnStart(
    messages: ChatMessage[],
    toolSpecs: ReadonlyArray<unknown> | undefined | null,
    model: string,
  ): { estimateTokens: number; ctxMax: number; ratio: number } {
    const ctxMax = resolveContextTokens(model);
    const estimate = estimateRequestTokens(messages, toolSpecs ?? null, true);
    return { estimateTokens: estimate, ctxMax, ratio: estimate / ctxMax };
  }

  async fold(
    model: string,
    opts?: { keepRecentTokens?: number; requireTailBoundary?: boolean },
  ): Promise<FoldResult> {
    const ctxMax = resolveContextTokens(model);
    const tailBudget = opts?.keepRecentTokens ?? Math.floor(ctxMax * HISTORY_FOLD_TAIL_FRACTION);
    const all = this.deps.log.toFullHistory();
    const noop: FoldResult = {
      folded: false,
      beforeMessages: all.length,
      afterMessages: all.length,
      summaryChars: 0,
    };
    if (all.length === 0) return noop;

    // Per-message token cost includes tool_calls JSON; otherwise heavy tool-call
    // arguments slip through the tail-budget check and the boundary slides past
    // the active tool turn. No chat-template wrapper here — that would double-count.
    const tokenCounts = all.map((m) => {
      let n = countTokensBounded(typeof m.content === "string" ? m.content : "");
      if (m.role === "assistant" && Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
        n += countTokensBounded(JSON.stringify(m.tool_calls));
      }
      return n;
    });
    const totalTokens = tokenCounts.reduce((a, b) => a + b, 0);

    let cumTokens = 0;
    let boundary = all.length;
    for (let i = all.length - 1; i >= 0; i--) {
      if (cumTokens + tokenCounts[i]! > tailBudget) break;
      cumTokens += tokenCounts[i]!;
      if (all[i]!.role === "user") boundary = i;
    }
    if (boundary <= 0) return noop;
    // Preflight-only: refuse when no user landed in tail — the active tool turn
    // would be wiped. Default fold path (post-response) tolerates empty tail so
    // cache-aligned summary tests still exercise the "summarize all" shape.
    if (opts?.requireTailBoundary && boundary >= all.length) return noop;

    const head = all.slice(0, boundary);
    const tail = all.slice(boundary);
    const headTokens = totalTokens - cumTokens;
    if (headTokens < totalTokens * HISTORY_FOLD_MIN_SAVINGS_FRACTION) return noop;

    const { names: pinnedNames, bodies: pinnedBodies } = collectPinnedSkills(head);
    const summary = await this.summarizeForFold(head, pinnedNames);
    if (!summary.content) return noop;

    const memoTail =
      pinnedBodies.length > 0 ? `\n\n${SKILL_PIN_MEMO_HEADER}\n\n${pinnedBodies.join("\n\n")}` : "";
    const constraints = extractPinnedConstraints(this.deps.getSystemPrompt());
    const constraintTail = constraints
      ? `\n\n[PINNED CONSTRAINTS — preserved verbatim]\n\n${constraints}`
      : "";
    // Route via buildAssistantMessage so the synthetic summary carries
    // reasoning_content under thinking-mode sessions — without it the
    // next API call 400s with "must be passed back" (#1042). Stamp uses
    // the SESSION model so an empty placeholder is added even when the
    // summarizer call somehow returned no reasoning.
    const summaryMsg = buildAssistantMessage(
      HISTORY_FOLD_MARKER + summary.content + memoTail + constraintTail,
      [],
      model,
      summary.reasoningContent,
    );
    const replacement = [summaryMsg, ...tail];
    this.deps.log.compactInPlace(replacement);
    this.persistRewrite(replacement);
    this.deps.onLogRewrite?.();
    return {
      folded: true,
      beforeMessages: all.length,
      afterMessages: replacement.length,
      summaryChars: summary.content.length,
    };
  }

  /** Drop a trailing in-flight assistant-with-tool_calls before a forced summary. Tail-only mutation; prefix cache safe. */
  trimTrailingToolCalls(): boolean {
    const tail = this.deps.log.entries[this.deps.log.entries.length - 1];
    if (
      !tail ||
      tail.role !== "assistant" ||
      !Array.isArray(tail.tool_calls) ||
      tail.tool_calls.length === 0
    ) {
      return false;
    }
    const kept = this.deps.log.entries.slice(0, -1);
    this.deps.log.compactInPlace([...kept]);
    this.persistRewrite([...kept]);
    return true;
  }

  private async summarizeForFold(
    messagesToSummarize: ChatMessage[],
    pinnedSkillNames: string[],
  ): Promise<{ content: string; reasoningContent: string }> {
    const summaryModel = "deepseek-v4-flash";
    const healed = healLoadedMessages(messagesToSummarize, DEFAULT_MAX_RESULT_CHARS).messages;
    const agentSystem = this.deps.getSystemPrompt();
    const fewShots = this.deps.getFewShots?.() ?? [];
    const tools = this.deps.getToolSpecs?.() ?? [];
    const instruction = buildFoldSummaryInstruction(pinnedSkillNames);
    const messages: ChatMessage[] = [
      { role: "system", content: agentSystem },
      ...fewShots.map((m) => ({ ...m })),
      ...healed,
      { role: "user", content: instruction },
    ];
    const turnSignal = this.deps.getAbortSignal();
    const foldCtrl = new AbortController();
    let cleanupAbort = (): void => {};
    let timeout: ReturnType<typeof setTimeout> | undefined;
    try {
      const abortPromise = new Promise<never>((_, reject) => {
        const abort = () => {
          foldCtrl.abort();
          reject(new Error("fold-aborted"));
        };
        if (turnSignal.aborted) {
          abort();
        } else {
          turnSignal.addEventListener("abort", abort, { once: true });
          cleanupAbort = () => turnSignal.removeEventListener("abort", abort);
        }
      });
      const timeoutPromise = new Promise<never>((_, reject) => {
        timeout = setTimeout(() => {
          foldCtrl.abort();
          reject(new Error("fold-timeout"));
        }, HISTORY_FOLD_SUMMARY_TIMEOUT_MS);
      });
      const resp = await Promise.race([
        this.deps.client.chat({
          model: summaryModel,
          messages,
          tools: tools.length ? (tools as ToolSpec[]) : undefined,
          signal: foldCtrl.signal,
          thinking: "disabled",
        }),
        abortPromise,
        timeoutPromise,
      ]);
      this.deps.stats.record(this.deps.getCurrentTurn(), summaryModel, resp.usage ?? new Usage());
      return {
        content: stripHallucinatedToolMarkup((resp.content ?? "").trim()),
        reasoningContent: resp.reasoningContent ?? "",
      };
    } catch {
      return { content: "", reasoningContent: "" };
    } finally {
      if (timeout) clearTimeout(timeout);
      cleanupAbort();
    }
  }

  private persistRewrite(messages: ChatMessage[]): void {
    if (!this.deps.sessionName) return;
    try {
      rewriteSession(this.deps.sessionName, messages);
    } catch {
      /* disk full / perms — in-memory mutation still applies */
    }
  }
}
