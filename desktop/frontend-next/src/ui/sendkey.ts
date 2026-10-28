import { t } from "../i18n";
import type { SendShortcut } from "../state/prefs";
import { ariaChord, chord, pressedChord } from "./keys";

type Key = { key: string; code: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean };

export function sendsOnKey(event: Key, mode: SendShortcut, touch = false): boolean {
  if (mode === "modifier_enter") return pressedChord(event, "Enter");
  return !touch && event.key === "Enter" && !event.shiftKey;
}

export function sendAria(mode: SendShortcut, touch = false): string | undefined {
  return mode === "modifier_enter" ? ariaChord("Enter") : touch ? undefined : "Enter Shift+Enter";
}

export function sendHint(mode: SendShortcut, running: boolean, touch: boolean): string {
  if (touch) return t(running ? "点按插话 · 回车换行" : "点按发送 · 回车换行");
  if (mode === "modifier_enter") return t(running ? "{key} 插话 · Enter 换行" : "{key} 发送 · Enter 换行", { key: chord("Enter") });
  return t(running ? "Enter 插话 · Shift+Enter 换行" : "Enter 发送 · Shift+Enter 换行");
}

// What pressing send will do. A turn can end while the kernel is still
// delivering its TurnDone, and admission stays shut until it lands: a line sent
// then joins the follow-up queue instead of starting a turn. `busy` is that
// fact, and `running` alone cannot report it. An older kernel sends no `busy`,
// and there the send is what it always was.
export type SendAction = "steer" | "queue" | "send";

export function sendAction(running: boolean, busy?: boolean): SendAction {
  if (running) return "steer";
  return busy ? "queue" : "send";
}

/** The send button's word for that state, so the composer's state and its label
 *  cannot disagree. */
export function sendLabel(submitting: boolean, running: boolean, busy?: boolean): string {
  if (submitting) return t("正在发送…");
  const as = sendAction(running, busy);
  return t(as === "steer" ? "插话" : as === "queue" ? "排队发送" : "发送");
}
