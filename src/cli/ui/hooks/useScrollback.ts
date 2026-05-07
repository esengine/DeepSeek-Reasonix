import { useMemo } from "react";
import type { DoctorCheckEntry, PlanStep } from "../state/cards.js";
import { useDispatch } from "../state/provider.js";

let seq = 0;
function nextId(prefix: string): string {
  seq += 1;
  return `${prefix}-${Date.now()}-${seq}`;
}

function formatTok(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}K`;
  return String(n);
}

export interface Scrollback {
  pushUser(text: string): string;
  pushWarning(title: string, message: string): string;
  pushError(title: string, message: string, stack?: string): string;
  pushInfo(
    text: string,
    tone?: "info" | "ok" | "warn" | "err" | "ghost" | "brand" | "accent",
  ): string;
  /** Emits a `ctxPressure` live card when usedTokens crosses 80% (warn) or 95% (err) of ctxMax. */
  pushCtxPressureIfHigh(usedTokens: number, ctxMax: number): void;
  pushStepProgress(stepIndex: number, total: number, title: string, elapsedMs?: number): string;
  pushPlanAnnounce(text: string): string;
  showDoctor(checks: ReadonlyArray<DoctorCheckEntry>): string;
  /** Emits a verbose Usage card (full bars) — used by `/cost`; auto-emitted per-turn cards stay compact. */
  showUsageVerbose(args: {
    turn: number;
    promptTokens: number;
    reasonTokens: number;
    outputTokens: number;
    promptCap: number;
    cacheHit: number;
    cost: number;
    sessionCost: number;
    balance?: number;
    balanceCurrency?: string;
    elapsedMs?: number;
  }): string;
  showPlan(args: {
    title: string;
    steps: PlanStep[];
    variant: "active" | "resumed" | "replay";
  }): string;
  completePlanStep(stepId: string): void;
  showCtx(args: {
    text: string;
    systemTokens: number;
    toolsTokens: number;
    logTokens: number;
    inputTokens: number;
    ctxMax: number;
    toolsCount: number;
    logMessages: number;
    topTools: ReadonlyArray<{ name: string; tokens: number; turn: number }>;
  }): string;

  startBranch(total: number): string;
  updateBranch(
    id: string,
    p: {
      completed: number;
      total: number;
      latestIndex: number;
      latestTemperature: number;
      latestUncertainties: number;
    },
  ): void;
  endBranch(id: string, aborted?: boolean): void;

  startReasoning(model?: string): string;
  appendReasoning(id: string, chunk: string): void;
  endReasoning(id: string, paragraphs: number, tokens: number, aborted?: boolean): void;

  startStreaming(model?: string): string;
  appendStreaming(id: string, chunk: string): void;
  endStreaming(id: string, aborted?: boolean): void;

  startTool(name: string, args: unknown): string;
  appendToolOutput(id: string, chunk: string): void;
  endTool(
    id: string,
    info: { output?: string; exitCode?: number; elapsedMs: number; aborted?: boolean },
  ): void;
  retryTool(id: string, attempt: number, max: number): void;

  thinking(): string;
  abortTurn(): void;
  endTurn(
    usage: {
      prompt: number;
      reason: number;
      output: number;
      cacheHit: number;
      cost: number;
    },
    extras?: { promptCap?: number; elapsedMs?: number },
  ): void;
  /** Wipe every card + toast — used by /clear and /new. */
  reset(): void;
}

export function useScrollback(): Scrollback {
  const dispatch = useDispatch();

  return useMemo<Scrollback>(
    () => ({
      pushUser(text) {
        const id = nextId("u");
        dispatch({ type: "user.submit", text });
        return id;
      },
      pushWarning(title, message) {
        const id = nextId("warn");
        dispatch({
          type: "live.show",
          id,
          ts: Date.now(),
          variant: "ctxPressure",
          tone: "warn",
          text: title,
          meta: message,
        });
        return id;
      },
      pushError(title, message, stack) {
        const id = nextId("err");
        dispatch({
          type: "live.show",
          id,
          ts: Date.now(),
          variant: "aborted",
          tone: "err",
          text: title,
          meta: stack ? `${message}\n${stack}` : message,
        });
        return id;
      },
      pushInfo(text, tone = "info") {
        const id = nextId("info");
        dispatch({
          type: "live.show",
          id,
          ts: Date.now(),
          variant: "stepProgress",
          tone,
          text,
        });
        return id;
      },
      pushCtxPressureIfHigh(usedTokens, ctxMax) {
        if (ctxMax <= 0) return;
        const pct = (usedTokens / ctxMax) * 100;
        if (pct < 80) return;
        const tone: "warn" | "err" = pct >= 95 ? "err" : "warn";
        const used = formatTok(usedTokens);
        const max = formatTok(ctxMax);
        dispatch({
          type: "live.show",
          id: nextId("ctxp"),
          ts: Date.now(),
          variant: "ctxPressure",
          tone,
          text: `Context  ${used} / ${max}  ·  ${pct.toFixed(0)}%`,
          meta:
            pct >= 95
              ? "trimming oldest turns to fit; expect short-term memory loss"
              : "approaching the budget; older turns will be dropped past 95%",
        });
      },
      pushStepProgress(stepIndex, total, title, elapsedMs) {
        const id = nextId("step");
        const meta = elapsedMs !== undefined ? `${(elapsedMs / 1000).toFixed(1)}s · done` : "done";
        dispatch({
          type: "live.show",
          id,
          ts: Date.now(),
          variant: "stepProgress",
          tone: "ok",
          text: `Step ${stepIndex} of ${total}  ·  ${title}`,
          meta,
        });
        return id;
      },
      pushPlanAnnounce(text) {
        const id = nextId("plan");
        dispatch({
          type: "live.show",
          id,
          ts: Date.now(),
          variant: "stepProgress",
          tone: "accent",
          text: "⊞ Plan submitted",
          meta: text.slice(0, 80),
        });
        return id;
      },
      showDoctor(checks) {
        const id = nextId("doc");
        dispatch({ type: "doctor.show", id, checks: [...checks] });
        return id;
      },
      showUsageVerbose(args) {
        const id = nextId("cost");
        dispatch({
          type: "usage.show",
          id,
          turn: args.turn,
          tokens: {
            prompt: args.promptTokens,
            reason: args.reasonTokens,
            output: args.outputTokens,
            promptCap: args.promptCap,
          },
          cacheHit: args.cacheHit,
          cost: args.cost,
          sessionCost: args.sessionCost,
          balance: args.balance,
          balanceCurrency: args.balanceCurrency,
          elapsedMs: args.elapsedMs,
        });
        return id;
      },
      showPlan({ title, steps, variant }) {
        const id = nextId("plan");
        dispatch({ type: "plan.show", id, title, steps, variant });
        return id;
      },
      completePlanStep(stepId) {
        dispatch({ type: "plan.step.complete", stepId });
      },
      showCtx(args) {
        const id = nextId("ctx");
        dispatch({ type: "ctx.show", id, ...args, topTools: [...args.topTools] });
        return id;
      },
      startBranch(total) {
        const id = nextId("branch");
        dispatch({ type: "branch.start", id, total });
        return id;
      },
      updateBranch(id, p) {
        dispatch({ type: "branch.progress", id, ...p });
      },
      endBranch(id, aborted) {
        dispatch({ type: "branch.end", id, aborted });
      },
      startReasoning(model) {
        const id = nextId("r");
        dispatch({ type: "reasoning.start", id, ...(model ? { model } : {}) });
        return id;
      },
      appendReasoning(id, chunk) {
        if (chunk.length > 0) dispatch({ type: "reasoning.chunk", id, text: chunk });
      },
      endReasoning(id, paragraphs, tokens, aborted) {
        dispatch({ type: "reasoning.end", id, paragraphs, tokens, aborted });
      },
      startStreaming(model) {
        const id = nextId("s");
        dispatch({ type: "streaming.start", id, ...(model ? { model } : {}) });
        return id;
      },
      appendStreaming(id, chunk) {
        if (chunk.length > 0) dispatch({ type: "streaming.chunk", id, text: chunk });
      },
      endStreaming(id, aborted) {
        dispatch({ type: "streaming.end", id, aborted });
      },
      startTool(name, args) {
        const id = nextId("tool");
        dispatch({ type: "tool.start", id, name, args });
        return id;
      },
      appendToolOutput(id, chunk) {
        if (chunk.length > 0) dispatch({ type: "tool.chunk", id, text: chunk });
      },
      endTool(id, info) {
        dispatch({
          type: "tool.end",
          id,
          output: info.output,
          exitCode: info.exitCode,
          elapsedMs: info.elapsedMs,
          aborted: info.aborted,
        });
      },
      retryTool(id, attempt, max) {
        dispatch({ type: "tool.retry", id, attempt, max });
      },
      thinking() {
        const id = nextId("think");
        dispatch({ type: "turn.thinking" });
        return id;
      },
      abortTurn() {
        dispatch({ type: "turn.abort" });
      },
      endTurn(usage, extras) {
        dispatch({
          type: "turn.end",
          usage,
          promptCap: extras?.promptCap,
          elapsedMs: extras?.elapsedMs,
        });
      },
      reset() {
        dispatch({ type: "session.reset" });
      },
    }),
    [dispatch],
  );
}
