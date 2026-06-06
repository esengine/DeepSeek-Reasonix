import type { Color } from "ink";
import { useEffect, useRef, useState } from "react";
import { t } from "../../i18n/index.js";
import type { LoopEvent } from "../../loop.js";
import { appendUsage } from "../../telemetry/usage.js";
import {
  SHARED_SUBAGENT_SINK,
  type SubagentEvent,
  type SubagentSink,
} from "../../tools/subagent.js";
import type { Scrollback } from "./hooks/useScrollback.js";
import { CARD, TONE, formatCost } from "./theme/tokens.js";

/** Identity-preserving — returns prev unchanged when no row would change. */
export function reduceSubagentInnerEvent(
  prev: ReadonlyArray<SubagentActivity>,
  ev: SubagentEvent,
): ReadonlyArray<SubagentActivity> {
  if (ev.kind === "inner") {
    if (!ev.inner) return prev;
    const summary = summariseInner(ev.inner);
    if (!summary) return prev;
    return mapMatchingRun(prev, ev.runId, (a) => ({ ...a, lastInner: summary }));
  }
  if (ev.kind === "progress") {
    return mapMatchingRun(prev, ev.runId, (a) => {
      const iter = ev.iter ?? a.iter;
      const elapsedMs = ev.elapsedMs ?? a.elapsedMs;
      if (iter === a.iter && elapsedMs === a.elapsedMs) return a;
      return { ...a, iter, elapsedMs };
    });
  }
  if (ev.kind === "phase") {
    return mapMatchingRun(prev, ev.runId, (a) => {
      const phase = ev.phase ?? a.phase;
      if (phase === a.phase) return a;
      return { ...a, phase };
    });
  }
  if (ev.kind === "stream-progress") {
    return mapMatchingRun(prev, ev.runId, (a) => {
      const outputChars = ev.outputChars ?? a.outputChars;
      const reasoningChars = ev.reasoningChars ?? a.reasoningChars;
      const toolReadChars = ev.toolReadChars ?? a.toolReadChars;
      const elapsedMs = ev.elapsedMs ?? a.elapsedMs;
      if (
        outputChars === a.outputChars &&
        reasoningChars === a.reasoningChars &&
        toolReadChars === a.toolReadChars &&
        elapsedMs === a.elapsedMs
      ) {
        return a;
      }
      return { ...a, outputChars, reasoningChars, toolReadChars, elapsedMs };
    });
  }
  return prev;
}

function mapMatchingRun(
  prev: ReadonlyArray<SubagentActivity>,
  runId: string,
  fn: (a: SubagentActivity) => SubagentActivity,
): ReadonlyArray<SubagentActivity> {
  let idx = -1;
  for (let i = 0; i < prev.length; i++) {
    if (prev[i]!.runId === runId) {
      idx = i;
      break;
    }
  }
  if (idx < 0) return prev;
  const updated = fn(prev[idx]!);
  if (updated === prev[idx]) return prev;
  const next = prev.slice();
  next[idx] = updated;
  return next;
}

function summariseInner(ev: LoopEvent): SubagentInnerSummary | null {
  if (ev.role === "tool_start") {
    return {
      glyph: "▣",
      color: CARD.tool.color,
      label: ev.toolName ?? t("common.tool"),
      meta: t("common.running"),
    };
  }
  if (ev.role === "tool") {
    return {
      glyph: "▣",
      color: CARD.tool.color,
      label: ev.toolName ?? t("common.tool"),
      meta: t("common.done"),
    };
  }
  if (ev.role === "warning") {
    return {
      glyph: "\u26a0",
      color: TONE.warn,
      label: t("common.warning"),
      meta: ev.content?.slice(0, 40),
    };
  }
  if (ev.role === "error") {
    return { glyph: "\u2716", color: TONE.err, label: ev.error ?? t("common.error") };
  }
  return null;
}

export interface SubagentInnerSummary {
  /** Card-kind-ish glyph (◆ reasoning, ▣ tool, ▶ streaming, ✖ error). */
  glyph: string;
  color: Color;
  label: string;
  meta?: string;
}

export interface SubagentActivity {
  /** Stable per-spawn id; key for parallel-row rendering. */
  runId: string;
  /** Wall-clock start so the stack stays in launch order even when events arrive interleaved. */
  startedAt: number;
  task: string;
  iter: number;
  elapsedMs: number;
  skillName?: string;
  model?: string;
  phase?: "exploring" | "summarising";
  lastInner: SubagentInnerSummary | null;
  /** Monotonic byte/char counters — proves data is flowing during long no-tool gaps. `outputChars` = model assistant content streamed, `reasoningChars` = model reasoning streamed, `toolReadChars` = sum of tool-result strings the child read back IN. */
  outputChars: number;
  reasoningChars: number;
  toolReadChars: number;
}

export interface UseSubagentParams {
  session: string | undefined;
  log: Scrollback;
  /** Read live wallet currency at end-event time so the cost suffix follows the wallet symbol. */
  getWalletCurrency?: () => string | undefined;
  /** Called when a subagent completes successfully — lets the caller fold
   *  subagent usage into the parent session's stats. (#2008) */
  onSubagentEnd?: (model: string, usage: import("../../client.js").Usage) => void;
}

export interface UseSubagentResult {
  /** In-flight runs, oldest first. Empty when none active. */
  activities: ReadonlyArray<SubagentActivity>;
  sinkRef: React.MutableRefObject<SubagentSink>;
}

export function useSubagent({
  session,
  log,
  getWalletCurrency,
  onSubagentEnd,
}: UseSubagentParams): UseSubagentResult {
  const [activities, setActivities] = useState<ReadonlyArray<SubagentActivity>>([]);
  // Share the process-wide singleton so `buildCodeToolset`'s pre-mount
  // `subagentRunner` closure (which reads sink.current at dispatch time)
  // sees the same `.current` we install below in useEffect.
  const sinkRef = useRef<SubagentSink>(SHARED_SUBAGENT_SINK);
  // Subagent runs can outlive a balance refresh; the thunk lives in a ref so the
  // sink callback (installed once at mount) always reads the latest wallet currency.
  const getWalletCurrencyRef = useRef(getWalletCurrency);
  useEffect(() => {
    getWalletCurrencyRef.current = getWalletCurrency;
  }, [getWalletCurrency]);
  const onSubagentEndRef = useRef(onSubagentEnd);
  useEffect(() => {
    onSubagentEndRef.current = onSubagentEnd;
  }, [onSubagentEnd]);

  useEffect(() => {
    sinkRef.current.current = (ev: SubagentEvent) => {
      if (ev.kind === "start") {
        setActivities((prev) => {
          if (prev.some((a) => a.runId === ev.runId)) return prev;
          const next: SubagentActivity = {
            runId: ev.runId,
            startedAt: Date.now() - (ev.elapsedMs ?? 0),
            task: ev.task,
            iter: ev.iter ?? 0,
            elapsedMs: ev.elapsedMs ?? 0,
            skillName: ev.skillName,
            model: ev.model,
            phase: "exploring",
            lastInner: null,
            outputChars: 0,
            reasoningChars: 0,
            toolReadChars: 0,
          };
          return [...prev, next];
        });
        return;
      }
      if (ev.kind === "end") {
        setActivities((prev) => prev.filter((a) => a.runId !== ev.runId));
        const seconds = ((ev.elapsedMs ?? 0) / 1000).toFixed(1);
        const costTail =
          ev.costUsd !== undefined && ev.costUsd > 0
            ? ` · ${formatCost(ev.costUsd, getWalletCurrencyRef.current?.())}`
            : "";
        const summary = ev.error
          ? `⌬ subagent "${ev.task}" failed after ${seconds}s · ${ev.iter ?? 0} tool call(s) — ${ev.error}`
          : `⌬ subagent "${ev.task}" done in ${seconds}s · ${ev.iter ?? 0} tool call(s) · ${ev.turns ?? 0} turn(s)${costTail}`;
        log.pushInfo(summary);
        if (!ev.error && ev.usage && ev.model) {
          appendUsage({
            session: session ?? null,
            model: ev.model,
            usage: ev.usage,
            kind: "subagent",
            subagent: {
              skillName: ev.skillName,
              taskPreview: ev.task.slice(0, 60),
              toolIters: ev.iter ?? 0,
              durationMs: ev.elapsedMs ?? 0,
            },
          });
          onSubagentEndRef.current?.(ev.model, ev.usage);
        }
        return;
      }
      setActivities((prev) => reduceSubagentInnerEvent(prev, ev));
    };
    return () => {
      sinkRef.current.current = null;
    };
  }, [session, log]);

  return { activities, sinkRef };
}
