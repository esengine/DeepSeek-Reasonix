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
