import { useEffect, useRef, useState } from "react";
import type { LoopEvent } from "../../loop.js";
import { appendUsage } from "../../telemetry/usage.js";
import type { SubagentEvent, SubagentSink } from "../../tools/subagent.js";
import type { Scrollback } from "./hooks/useScrollback.js";
import { CARD, TONE, formatCost } from "./theme/tokens.js";

function summariseInner(ev: LoopEvent): SubagentInnerSummary | null {
  if (ev.role === "tool_start") {
    return {
      glyph: "▣",
      color: CARD.tool.color,
      label: ev.toolName ?? "tool",
      meta: "running",
    };
  }
  if (ev.role === "tool") {
    return {
      glyph: "▣",
      color: CARD.tool.color,
      label: ev.toolName ?? "tool",
      meta: "done",
    };
  }
  if (ev.role === "warning") {
    return { glyph: "⚠", color: TONE.warn, label: "warning", meta: ev.content?.slice(0, 40) };
  }
  if (ev.role === "error") {
    return { glyph: "✖", color: TONE.err, label: ev.error ?? "error" };
  }
  return null;
}

export interface SubagentInnerSummary {
  /** Card-kind-ish glyph (◆ reasoning, ▣ tool, ▶ streaming, ✖ error). */
  glyph: string;
  color: string;
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
}

export interface UseSubagentParams {
  session: string | undefined;
  log: Scrollback;
  /** Read live wallet currency at end-event time so the cost suffix follows the wallet symbol. */
  getWalletCurrency?: () => string | undefined;
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
}: UseSubagentParams): UseSubagentResult {
  const [activities, setActivities] = useState<ReadonlyArray<SubagentActivity>>([]);
  const sinkRef = useRef<SubagentSink>({ current: null });
  // Subagent runs can outlive a balance refresh; the thunk lives in a ref so the
  // sink callback (installed once at mount) always reads the latest wallet currency.
  const getWalletCurrencyRef = useRef(getWalletCurrency);
  useEffect(() => {
    getWalletCurrencyRef.current = getWalletCurrency;
  }, [getWalletCurrency]);

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
        }
        return;
      }
      // progress / phase / inner — patch the matching row, ignore stragglers from runs we never saw `start` for.
      setActivities((prev) =>
        prev.map((a) => {
          if (a.runId !== ev.runId) return a;
          if (ev.kind === "progress") {
            return {
              ...a,
              iter: ev.iter ?? a.iter,
              elapsedMs: ev.elapsedMs ?? a.elapsedMs,
            };
          }
          if (ev.kind === "phase") {
            return { ...a, phase: ev.phase ?? a.phase };
          }
          if (ev.kind === "inner" && ev.inner) {
            const summary = summariseInner(ev.inner);
            return summary ? { ...a, lastInner: summary } : a;
          }
          return a;
        }),
      );
    };
    return () => {
      sinkRef.current.current = null;
    };
  }, [session, log]);

  return { activities, sinkRef };
}
