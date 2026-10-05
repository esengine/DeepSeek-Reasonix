import type { WireEvent } from "../port/wire";
import { t } from "../i18n";
import { nextId } from "./ids";
import type { SessionState, Stall } from "./session_types";

// Client gestures on the strip. Muting belongs to this session's window and
// hides the running notice only: a pause the user asked for is still said.
export type StallGesture = { kind: "__stall_mute" } | { kind: "__stall_dismiss" };

/** The kernel's progress-watch reading, held while it is true. Nothing here
 *  decides a run is stalled: the frame says so, and a frame saying it cleared,
 *  or the turn ending any way but paused, is what takes it down. */
export function foldStall(s: SessionState, ev: WireEvent | StallGesture | { kind: string }): SessionState {
  switch (ev.kind) {
    case "__stall_mute":
      return { ...s, stallMuted: true };
    case "__stall_dismiss":
    case "turn_started":
      return s.stall ? { ...s, stall: undefined } : s;
    case "progress_watch": {
      const w = (ev as WireEvent).progressWatch;
      if (!w?.stalled || !w.cause) return s.stall ? { ...s, stall: undefined } : s;
      const stall: Stall = {
        cause: w.cause, idleRounds: w.idleRounds, roundLimit: w.roundLimit ?? 0,
        promptTokens: w.promptTokens ?? 0, tokenMultiple: w.tokenMultiple ?? 0, paused: !!w.pausing,
      };
      return { ...s, stall };
    }
    case "turn_done": {
      if ((ev as WireEvent).outcome !== "no_progress") return s.stall ? { ...s, stall: undefined } : s;
      // The strip can be dismissed; the transcript keeps why this turn stopped.
      const note = { t: "notice" as const, id: nextId(), level: "info" as const, text: pausedNote(s.stall) };
      return { ...s, stall: s.stall && { ...s.stall, paused: true }, items: [...s.items, note] };
    }
    default:
      return s;
  }
}

function pausedNote(stall?: Stall): string {
  if (stall?.cause === "tokens") return t("任务已按设置暂停：自上次有进展以来的输入已达上下文窗口的 {n} 倍", { n: stall.tokenMultiple });
  if (stall?.cause === "perseveration") return t("任务已按设置暂停：模型卡在重复输出同一段文字");
  return t("任务已按设置暂停：连续 {n} 轮没有可观察的进展", { n: stall?.idleRounds ?? 0 });
}

/** Whether the strip draws: a pause always, a running stall until muted. */
export const stallShown = (s: Pick<SessionState, "stall" | "stallMuted">): Stall | undefined =>
  s.stall && (s.stall.paused || !s.stallMuted) ? s.stall : undefined;
