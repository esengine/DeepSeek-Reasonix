import { t } from "../i18n";
import type { RunningMessageIntent, SendShortcut } from "../state/prefs";
import { ariaChord, chord, pressedChord } from "./keys";

type Key = { key: string; code: string; metaKey: boolean; ctrlKey: boolean; altKey: boolean; shiftKey: boolean };

export function sendsOnKey(event: Key, mode: SendShortcut, touch = false): boolean {
  if (mode === "modifier_enter") return pressedChord(event, "Enter");
  return !touch && event.key === "Enter" && !event.shiftKey;
}

export function sendAria(mode: SendShortcut, touch = false): string | undefined {
  return mode === "modifier_enter" ? ariaChord("Enter") : touch ? undefined : "Enter Shift+Enter";
}

// Steering carries plain text only, so a line with a skill card queues
// whatever the preference says; the button and hint name what will happen.
export function runningSendIntent(preferred: RunningMessageIntent, hasChips: boolean): RunningMessageIntent {
  return hasChips ? "followup" : preferred;
}

export function sendHint(mode: SendShortcut, running: boolean, touch: boolean, intent: RunningMessageIntent = "steer"): string {
  if (running && intent === "followup") {
    if (touch) return t("点按排队 · 回车换行");
    if (mode === "modifier_enter") return t("{key} 排队 · Enter 换行", { key: chord("Enter") });
    return t("Enter 排队 · Shift+Enter 换行");
  }
  if (touch) return t(running ? "点按插话 · 回车换行" : "点按发送 · 回车换行");
  if (mode === "modifier_enter") return t(running ? "{key} 插话 · Enter 换行" : "{key} 发送 · Enter 换行", { key: chord("Enter") });
  return t(running ? "Enter 插话 · Shift+Enter 换行" : "Enter 发送 · Shift+Enter 换行");
}
