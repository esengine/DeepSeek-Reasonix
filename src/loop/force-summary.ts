import { type DeepSeekClient, Usage } from "../client.js";
import { t } from "../i18n/index.js";
import type { TurnStats } from "../telemetry/stats.js";
import type { ChatMessage } from "../types.js";
import { errorLabelFor, reasonPrefixFor } from "./errors.js";
import { buildAssistantMessage } from "./messages.js";
import { stripHallucinatedToolMarkup, thinkingModeForModel } from "./thinking.js";
import type { LoopEvent } from "./types.js";

export type ForceSummaryReason = "budget" | "aborted" | "context-guard" | "stuck";

export interface ForceSummaryContext {
  client: DeepSeekClient;
  signal: AbortSignal;
  buildMessages: () => ChatMessage[];
  appendAndPersist: (msg: ChatMessage) => void;
  recordStats: (model: string, usage: Usage) => TurnStats;
  turn: number;
  maxToolIters: number;
}

export async function* forceSummaryAfterIterLimit(
  ctx: ForceSummaryContext,
  opts: { reason: ForceSummaryReason } = { reason: "budget" },
): AsyncGenerator<LoopEvent> {
  try {
    // Status bridges the silence — summary call is non-streaming, 30-60s typical.
    yield { turn: ctx.turn, role: "status", content: t("summary.status") };
    const messages = ctx.buildMessages();
    // Passing `tools: undefined` was supposed to force a text response,
    // but R1 can still hallucinate tool-call markup (e.g. DSML
    // `<｜DSML｜function_calls>…`) when primed by prior tool use. An
    // explicit user-role instruction plus post-hoc stripping of known
    // hallucination shapes keeps the user from seeing raw markup.
    messages.push({
      role: "user",
      content:
        "I'm out of tool-call budget for this turn. Summarize in plain prose what you learned from the tool results above. Do NOT emit any tool calls, function-call markup, DSML invocations, or SEARCH/REPLACE edit blocks — they will be silently discarded. Just plain text.",
    });
    // Pin to flash + effort=high regardless of the main turn's model —
    // pro is 12× overkill for "paraphrase tool results into prose," and
    // budget-exhausted turns are exactly when we don't want to torch the wallet.
    const summaryModel = "deepseek-v4-flash";
    const summaryEffort: "high" | "max" = "high";
    const resp = await ctx.client.chat({
      model: summaryModel,
      messages,
      signal: ctx.signal,
      thinking: thinkingModeForModel(summaryModel),
      reasoningEffort: summaryEffort,
    });
    const rawContent = resp.content?.trim() ?? "";
    const cleaned = stripHallucinatedToolMarkup(rawContent);
    const summary = cleaned || t("summary.hallucinatedFallback");
    const reasonPrefix = reasonPrefixFor(opts.reason, ctx.maxToolIters);
    const annotated = `${reasonPrefix}\n\n${summary}`;
    // Record under the actual model used (flash), so per-turn cost reflects reality.
    const summaryStats = ctx.recordStats(summaryModel, resp.usage ?? new Usage());
    ctx.appendAndPersist(buildAssistantMessage(summary, [], summaryModel, resp.reasoningContent));
    yield {
      turn: ctx.turn,
      role: "assistant_final",
      content: annotated,
      stats: summaryStats,
      forcedSummary: true,
    };
    yield { turn: ctx.turn, role: "done", content: summary };
  } catch (err) {
    const label = errorLabelFor(opts.reason, ctx.maxToolIters);
    yield {
      turn: ctx.turn,
      role: "error",
      content: "",
      error: t("summary.failedAfterReason", { label, message: (err as Error).message }),
    };
    yield { turn: ctx.turn, role: "done", content: "" };
  }
}
