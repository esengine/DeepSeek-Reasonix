import { type DeepSeekClient, Usage } from "../client.js";
import { t } from "../i18n/index.js";
import type { TurnStats } from "../telemetry/stats.js";
import type { ChatMessage } from "../types.js";
import { errorLabelFor, reasonPrefixFor } from "./errors.js";
import { buildAssistantMessage } from "./messages.js";
import { stripHallucinatedToolMarkup } from "./thinking.js";
import type { LoopEvent } from "./types.js";

export type ForceSummaryReason = "aborted" | "context-guard" | "stuck";

export interface ForceSummaryContext {
  client: DeepSeekClient;
  signal: AbortSignal;
  buildMessages: () => ChatMessage[];
  appendAndPersist: (msg: ChatMessage) => void;
  recordStats: (model: string, usage: Usage) => TurnStats;
  turn: number;
  /** Model to call for the summary itself — must be valid on the user's endpoint. */
  model: string;
}

export async function* forceSummaryAfterIterLimit(
  ctx: ForceSummaryContext,
  opts: { reason: ForceSummaryReason },
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
        "The turn is being force-summarized (context guard or stuck-state). Summarize in plain prose what you learned from the tool results above. Do NOT emit any tool calls, function-call markup, DSML invocations, or SEARCH/REPLACE edit blocks — they will be silently discarded. Just plain text.",
    });
    // Use the active turn model — pinning a specific name (e.g. flash) 400s
    // on third-party endpoints that don't advertise it. `thinking: disabled`
    // still keeps reasoning tokens off the bill for the bounded paraphrase.
    const resp = await ctx.client.chat({
      model: ctx.model,
      messages,
      signal: ctx.signal,
      thinking: "disabled",
    });
    const rawContent = resp.content?.trim() ?? "";
    const cleaned = stripHallucinatedToolMarkup(rawContent);
    const summary = cleaned || t("summary.hallucinatedFallback");
    const reasonPrefix = reasonPrefixFor(opts.reason);
    const annotated = `${reasonPrefix}\n\n${summary}`;
    const summaryStats = ctx.recordStats(ctx.model, resp.usage ?? new Usage());
    ctx.appendAndPersist(buildAssistantMessage(summary, [], ctx.model, resp.reasoningContent));
    yield {
      turn: ctx.turn,
      role: "assistant_final",
      content: annotated,
      stats: summaryStats,
      forcedSummary: true,
    };
    yield { turn: ctx.turn, role: "done", content: summary };
  } catch (err) {
    const label = errorLabelFor(opts.reason);
    const message = t("summary.failedAfterReason", { label, message: (err as Error).message });
    yield {
      turn: ctx.turn,
      role: "error",
      content: "",
      error: message,
      errorDetail: {
        name: "ForceSummaryFailed",
        message,
        retryable: true,
        recoverable: true,
      },
    };
    yield { turn: ctx.turn, role: "done", content: "" };
  }
}
