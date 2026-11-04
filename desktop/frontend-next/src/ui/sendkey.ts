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
    if (touch) return t("composer.tap_queue_enter_for_a");
    if (mode === "modifier_enter") return t("composer.key_to_queue_enter_for", { key: chord("Enter") });
    return t("composer.enter_to_queue_shift_enter");
  }
  if (touch) return t(running ? "composer.tap_steer_enter_for_a" : "composer.tap_send_enter_for_a");
  if (mode === "modifier_enter") return t(running ? "composer.key_to_steer_enter_for" : "composer.key_to_send_enter_for", { key: chord("Enter") });
  return t(running ? "composer.enter_to_steer_shift_enter" : "composer.enter_to_send_shift_enter");
}
