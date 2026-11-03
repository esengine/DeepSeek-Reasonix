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
    if (touch) return t("sendkey.tapQueueEnterForA");
    if (mode === "modifier_enter") return t("sendkey.toQueueEnterForA", { key: chord("Enter") });
    return t("sendkey.enterToQueueShiftEnter");
  }
  if (touch) return t(running ? "sendkey.tapSteerEnterForA" : "sendkey.tapSendEnterForA");
  if (mode === "modifier_enter") return t(running ? "sendkey.toSteerEnterForA" : "sendkey.toSendEnterForA", { key: chord("Enter") });
  return t(running ? "sendkey.enterToSteerShiftEnter" : "sendkey.enterToSendShiftEnter");
}
