import type { MutableRefObject } from "react";
import type { EditBlock } from "../../../code/edit-blocks.js";
import { clearPendingEdits } from "../../../code/pending-edits.js";
import type { SlashResult } from "../slash.js";
import type { Scrollback } from "./useScrollback.js";

export type SlashOutcome = { kind: "consumed" } | { kind: "resubmit"; text: string };

interface ActiveLoopShape {
  prompt: string;
  intervalMs: number;
  nextFireAt: number;
  iter: number;
}

export interface ApplySlashResultContext {
  log: Scrollback;
  stdoutWrite: (chunk: string) => void;
  pendingEdits: MutableRefObject<EditBlock[]>;
  syncPendingCount: () => void;
  session: string | null;
  codeModeOn: boolean;
  activeLoopRef: MutableRefObject<ActiveLoopShape | null>;
  stopLoop: () => void;
  quitProcess: () => void;
  pushHistory: (text: string) => void;
  /** The verbatim text the user typed; used for promptHistory bookkeeping. */
  text: string;
}

export function applySlashResult(result: SlashResult, ctx: ApplySlashResultContext): SlashOutcome {
  if (result.exit) {
    // Tear down /loop before quitProcess so the timer doesn't fire after
    // the process is exiting. Use quitProcess (process.exit) rather than
    // Ink's exit(): the singleton stdin reader keeps a `data` listener
    // attached, so exit() unmounts React but leaves the event loop alive.
    if (ctx.activeLoopRef.current) ctx.stopLoop();
    ctx.quitProcess();
    return { kind: "consumed" };
  }
  if (result.clear) {
    // 2J + 3J + H: nuke viewport AND scrollback (visible buffer + the
    // off-screen lines). Then reset state.cards so React doesn't
    // immediately repaint everything that was just wiped.
    ctx.stdoutWrite("\x1b[2J\x1b[3J\x1b[H");
    ctx.log.reset();
    if (result.info) ctx.log.pushInfo(result.info);
    if (ctx.codeModeOn) {
      ctx.pendingEdits.current = [];
      clearPendingEdits(ctx.session);
      ctx.syncPendingCount();
    }
    if (ctx.activeLoopRef.current) ctx.stopLoop();
    return { kind: "consumed" };
  }
  if (result.info) {
    if (result.ctxBreakdown) {
      ctx.log.showCtx({ text: result.info, ...result.ctxBreakdown });
    } else {
      ctx.log.pushInfo(result.info);
    }
  }
  if (result.replayPlan) {
    const rp = result.replayPlan;
    const done = new Set(rp.completedStepIds);
    const titleSuffix = rp.summary ? ` — ${rp.summary}` : "";
    ctx.log.showPlan({
      title: `Replay #${rp.index}/${rp.total} · ${rp.relativeTime}${titleSuffix}`,
      steps: rp.steps.map((s) => ({
        id: s.id,
        title: s.title,
        status: done.has(s.id) ? "done" : "queued",
      })),
      variant: "replay",
    });
  }
  if (result.resubmit) {
    return { kind: "resubmit", text: result.resubmit };
  }
  ctx.pushHistory(ctx.text);
  return { kind: "consumed" };
}
