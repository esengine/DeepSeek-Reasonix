import type { DeepSeekClient } from "./client.js";
import { Usage } from "./client.js";
import { healLoadedMessages } from "./loop.js";
import { thinkingModeForModel } from "./loop.js";
import { stripHallucinatedToolMarkup } from "./loop.js";
import { DEFAULT_MAX_RESULT_CHARS } from "./mcp/registry.js";
import type { AppendOnlyLog } from "./memory/runtime.js";
import { rewriteSession } from "./memory/session.js";
import {
  DEEPSEEK_CONTEXT_TOKENS,
  DEFAULT_CONTEXT_TOKENS,
  type SessionStats,
} from "./telemetry/stats.js";
import { estimateConversationTokens, estimateRequestTokens } from "./tokenizer.js";
import type { ChatMessage } from "./types.js";

/** Auto-fold when a turn's response shows promptTokens above this fraction of ctxMax. */
export const HISTORY_FOLD_THRESHOLD = 0.5;
/** Tail budget after a normal fold, as a fraction of ctxMax. */
export const HISTORY_FOLD_TAIL_FRACTION = 0.2;
/** Above this fraction the normal fold's tail budget didn't buy enough headroom — fold harder. */
export const HISTORY_FOLD_AGGRESSIVE_THRESHOLD = 0.7;
/** Tail budget after an aggressive fold — half the normal one, sacrifices recent context for headroom. */
export const HISTORY_FOLD_AGGRESSIVE_TAIL_FRACTION = 0.1;
/** Skip the fold if the head wouldn't shrink the log by at least this fraction. */
export const HISTORY_FOLD_MIN_SAVINGS_FRACTION = 0.3;
/** Above this fraction we exit the turn with a summary instead of folding (defense in depth). */
export const FORCE_SUMMARY_THRESHOLD = 0.8;
/** Local preflight estimate above this fraction trips the emergency in-place compact path. */
export const PREFLIGHT_EMERGENCY_THRESHOLD = 0.95;
/** Prepended to fold summary content so the model knows it's a synthesized recap. */
export const HISTORY_FOLD_MARKER =
  "[CONVERSATION HISTORY SUMMARY — earlier turns folded for context efficiency]\n\n";

export interface ContextManagerDeps {
  client: DeepSeekClient;
  log: AppendOnlyLog;
  stats: SessionStats;
  sessionName: string | null;
  getAbortSignal: () => AbortSignal;
  getCurrentTurn: () => number;
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

export interface PreflightDecision {
  needsAction: boolean;
  estimateTokens: number;
  ctxMax: number;
}

export interface FoldResult {
  folded: boolean;
  beforeMessages: number;
  afterMessages: number;
  summaryChars: number;
}

export class ContextManager {
  constructor(private deps: ContextManagerDeps) {}

  /** Decision after a turn's response — fold, exit with summary, or carry on. */
  decideAfterUsage(
    usage: Usage | null,
    model: string,
    alreadyFoldedThisTurn: boolean,
  ): PostUsageDecision {
    const ctxMax = DEEPSEEK_CONTEXT_TOKENS[model] ?? DEFAULT_CONTEXT_TOKENS;
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

  /** Local-side preflight before sending a request — catches oversized payloads early. */
  decidePreflight(
    messages: ChatMessage[],
    toolSpecs: ReadonlyArray<unknown> | undefined | null,
    model: string,
  ): PreflightDecision {
    const ctxMax = DEEPSEEK_CONTEXT_TOKENS[model] ?? DEFAULT_CONTEXT_TOKENS;
    const estimate = estimateRequestTokens(messages, toolSpecs ?? null);
    return {
      needsAction: estimate / ctxMax > PREFLIGHT_EMERGENCY_THRESHOLD,
      estimateTokens: estimate,
      ctxMax,
    };
  }

  /** Replace older turns with one summary message; keep tail within keepRecentTokens budget. */
  async fold(model: string, opts?: { keepRecentTokens?: number }): Promise<FoldResult> {
    const ctxMax = DEEPSEEK_CONTEXT_TOKENS[model] ?? DEFAULT_CONTEXT_TOKENS;
    const tailBudget = opts?.keepRecentTokens ?? Math.floor(ctxMax * HISTORY_FOLD_TAIL_FRACTION);
    const all = this.deps.log.toMessages();
    const noop: FoldResult = {
      folded: false,
      beforeMessages: all.length,
      afterMessages: all.length,
      summaryChars: 0,
    };
    if (all.length === 0) return noop;

    const tokenCounts = all.map((m) => estimateConversationTokens([m]));
    const totalTokens = tokenCounts.reduce((a, b) => a + b, 0);

    let cumTokens = 0;
    let boundary = all.length;
    for (let i = all.length - 1; i >= 0; i--) {
      if (cumTokens + tokenCounts[i]! > tailBudget) break;
      cumTokens += tokenCounts[i]!;
      if (all[i]!.role === "user") boundary = i;
    }
    if (boundary <= 0) return noop;

    const head = all.slice(0, boundary);
    const tail = all.slice(boundary);
    const headTokens = totalTokens - cumTokens;
    if (headTokens < totalTokens * HISTORY_FOLD_MIN_SAVINGS_FRACTION) return noop;

    const summary = await this.summarizeForFold(head);
    if (!summary) return noop;

    const summaryMsg: ChatMessage = {
      role: "assistant",
      content: HISTORY_FOLD_MARKER + summary,
    };
    const replacement = [summaryMsg, ...tail];
    this.deps.log.compactInPlace(replacement);
    this.persistRewrite(replacement);
    return {
      folded: true,
      beforeMessages: all.length,
      afterMessages: replacement.length,
      summaryChars: summary.length,
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

  private async summarizeForFold(messagesToSummarize: ChatMessage[]): Promise<string> {
    const summaryModel = "deepseek-v4-flash";
    const systemPrompt =
      "You compress conversation history for a coding agent. Output one prose recap that preserves: the user's overall goal, decisions and conclusions reached, files inspected or modified, important tool results still relevant to ongoing work, and any open todos. Skip turn-by-turn play-by-play. No tool calls, no markdown headings, no SEARCH/REPLACE blocks — plain prose only.";
    const healed = healLoadedMessages(messagesToSummarize, DEFAULT_MAX_RESULT_CHARS).messages;
    const messages: ChatMessage[] = [
      { role: "system", content: systemPrompt },
      ...healed,
      {
        role: "user",
        content:
          "Summarize the conversation above as plain prose. This summary replaces the original turns to free context — make it self-contained.",
      },
    ];
    try {
      const resp = await this.deps.client.chat({
        model: summaryModel,
        messages,
        signal: this.deps.getAbortSignal(),
        thinking: thinkingModeForModel(summaryModel),
        reasoningEffort: "high",
      });
      this.deps.stats.record(this.deps.getCurrentTurn(), summaryModel, resp.usage ?? new Usage());
      return stripHallucinatedToolMarkup((resp.content ?? "").trim());
    } catch {
      return "";
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
