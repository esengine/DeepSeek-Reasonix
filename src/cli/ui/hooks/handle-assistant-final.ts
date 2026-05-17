import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import {
  type ApplyResult,
  type EditBlock,
  type EditSnapshot,
  applyEditBlocks,
  parseEditBlocks,
  snapshotBeforeEdits,
} from "../../../code/edit-blocks.js";
import { savePendingEdits } from "../../../code/pending-edits.js";
import type { EditMode } from "../../../config.js";
import type { LoopEvent } from "../../../loop.js";
import type { DashboardEvent } from "../../../server/context.js";
import type { SessionSummary } from "../../../telemetry/stats.js";
import { appendUsage } from "../../../telemetry/usage.js";
import { formatEditResults, formatPendingPreview } from "../edit-history.js";
import type { TurnTranslator } from "../state/TurnTranslator.js";
import type { Scrollback } from "./useScrollback.js";

export interface AssistantFinalContext {
  flush: () => void;
  translator: TurnTranslator;
  streamRef: { text: string; reasoning: string; toolCallBuild?: { name: string; chars: number } };
  contentBuf: { current: string };
  reasoningBuf: { current: string };
  toolCallBuildBuf: {
    current: { name: string; chars: number; index?: number; readyCount?: number } | null;
  };
  assistantId: string;
  setSummary: Dispatch<SetStateAction<SessionSummary>>;
  log: Scrollback;
  broadcastDashboardEvent: (ev: DashboardEvent) => void;
  getSessionSummary: () => SessionSummary;
  session: string | null;
  assistantIterCounter: MutableRefObject<number>;
  codeModeOn: boolean;
  currentRootDir: string;
  editModeRef: MutableRefObject<EditMode>;
  recordEdit: (
    source: string,
    blocks: readonly EditBlock[],
    results: readonly ApplyResult[],
    snaps: readonly EditSnapshot[],
  ) => void;
  armUndoBanner: (results: ApplyResult[]) => void;
  pendingEdits: MutableRefObject<EditBlock[]>;
  syncPendingCount: () => void;
  /** Used to gate the ctx-pressure warn/err cards; 0 disables the check. */
  ctxMax: number;
}

export function handleAssistantFinal(ev: LoopEvent, ctx: AssistantFinalContext): void {
  ctx.flush();
  ctx.translator.reasoningDone(ctx.streamRef.reasoning);
  ctx.translator.streamingDone();
  ctx.broadcastDashboardEvent({
    kind: "assistant_final",
    id: ctx.assistantId,
    text: ev.content || ctx.streamRef.text,
    reasoning: ctx.streamRef.reasoning || undefined,
  });
  // Keep the live stats panel current with per-iter usage. Without this,
  // cost/ctx/cache/hit stay at the prior turn's numbers until the whole
  // step resolves — confusing in multi-iter tool-call chains.
  ctx.setSummary(ctx.getSessionSummary());
  if (ev.stats?.usage) {
    appendUsage({
      session: ctx.session,
      model: ev.stats.model,
      usage: ev.stats.usage,
    });
    // Pass the session-aggregate cache-hit so the persistent status bar
    // mirrors what the web dashboard reads from `loop.stats.summary()`
    // (issue #1028) instead of showing this single turn's ratio.
    ctx.translator.turnEnd(ev.stats, ctx.streamRef.reasoning, {
      promptCap: ctx.ctxMax > 0 ? ctx.ctxMax : undefined,
      sessionCacheHit: ctx.getSessionSummary().cacheHitRatio,
    });
    if (ctx.ctxMax > 0) {
      ctx.log.pushCtxPressureIfHigh(ev.stats.usage.promptTokens, ctx.ctxMax);
    }
  }
  const finalText = ev.content || ctx.streamRef.text;
  ctx.assistantIterCounter.current++;
  // streamRef is scoped to the whole handleSubmit call; reset between iters
  // so deltas don't bleed into the next.
  ctx.streamRef.text = "";
  ctx.streamRef.reasoning = "";
  ctx.streamRef.toolCallBuild = undefined;
  ctx.contentBuf.current = "";
  ctx.reasoningBuf.current = "";
  ctx.toolCallBuildBuf.current = null;

  if (!ctx.codeModeOn || !finalText || ev.forcedSummary) return;
  // ev.forcedSummary gates us out: forced summaries are wrap-ups, not plans
  // to execute, so SEARCH/REPLACE blocks inside are display-only.
  const blocks = parseEditBlocks(finalText);
  if (blocks.length === 0) return;

  if (ctx.editModeRef.current === "auto" || ctx.editModeRef.current === "yolo") {
    const snaps = snapshotBeforeEdits(blocks, ctx.currentRootDir);
    const results = applyEditBlocks(blocks, ctx.currentRootDir);
    const good = results.some((r) => r.status === "applied" || r.status === "created");
    if (good) {
      ctx.recordEdit("auto-text", blocks, results, snaps);
      ctx.armUndoBanner(results);
    }
    ctx.log.pushInfo(formatEditResults(results));
  } else {
    // Append, don't replace — tool-call edits earlier in the same turn
    // may already be queued via the registry interceptor.
    ctx.pendingEdits.current = [...ctx.pendingEdits.current, ...blocks];
    // Checkpoint the queue so a crash between "blocks parsed" and "user
    // /apply" doesn't lose the edits.
    savePendingEdits(ctx.session, ctx.pendingEdits.current);
    ctx.syncPendingCount();
    ctx.log.pushInfo(formatPendingPreview(ctx.pendingEdits.current));
  }
}
