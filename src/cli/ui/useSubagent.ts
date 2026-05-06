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
  /** Live state for an in-flight subagent, null when none is running. */
  activity: SubagentActivity | null;
  sinkRef: React.MutableRefObject<SubagentSink>;
}

export function useSubagent({
  session,
  log,
  getWalletCurrency,
}: UseSubagentParams): UseSubagentResult {
  const [activity, setActivity] = useState<SubagentActivity | null>(null);
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
        setActivity({
          task: ev.task,
          iter: ev.iter ?? 0,
          elapsedMs: ev.elapsedMs ?? 0,
          skillName: ev.skillName,
          model: ev.model,
          phase: "exploring",
          lastInner: null,
        });
        return;
      }
      if (ev.kind === "progress") {
        setActivity((prev) =>
          prev
            ? {
                ...prev,
                iter: ev.iter ?? prev.iter,
                elapsedMs: ev.elapsedMs ?? prev.elapsedMs,
              }
            : {
                task: ev.task,
                iter: ev.iter ?? 0,
                elapsedMs: ev.elapsedMs ?? 0,
                skillName: ev.skillName,
                model: ev.model,
                phase: "exploring",
                lastInner: null,
              },
        );
        return;
      }
      if (ev.kind === "phase") {
        setActivity((prev) => (prev ? { ...prev, phase: ev.phase } : prev));
        return;
      }
      if (ev.kind === "inner" && ev.inner) {
        const summary = summariseInner(ev.inner);
        if (!summary) return;
        setActivity((prev) => (prev ? { ...prev, lastInner: summary } : prev));
        return;
      }
      // end
      setActivity(null);
      const seconds = ((ev.elapsedMs ?? 0) / 1000).toFixed(1);
      // Inline cost: the one number most users look at. Saves a /stats round-trip.
      const costTail =
        ev.costUsd !== undefined && ev.costUsd > 0
          ? ` · ${formatCost(ev.costUsd, getWalletCurrencyRef.current?.())}`
          : "";
      const summary = ev.error
        ? `⌬ subagent "${ev.task}" failed after ${seconds}s · ${ev.iter ?? 0} tool call(s) — ${ev.error}`
        : `⌬ subagent "${ev.task}" done in ${seconds}s · ${ev.iter ?? 0} tool call(s) · ${ev.turns ?? 0} turn(s)${costTail}`;
      log.pushInfo(summary);
      // Persist a subagent summary row to ~/.reasonix/usage.jsonl so
      // `/stats` and `reasonix stats` surface it. Skipped on error —
      // we only record what actually cost money and did work.
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
    };
    return () => {
      sinkRef.current.current = null;
    };
  }, [session, log]);

  return { activity, sinkRef };
}
