// useTopicShortcuts - Cmd/Ctrl hold detection plus 1-9 sidebar topic navigation.
//
// When the user holds Cmd (macOS) or Ctrl (Windows/Linux) for a brief moment
// without pressing another key, shortcut badges appear over the sidebar topic
// list. Releasing the modifier hides them immediately. Pressing Cmd/Ctrl+1-9
// navigates to the matching topic.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  defaultShortcutCombo,
  detectShortcutPlatform,
  formatShortcutCombo,
  isShortcutRecorderTarget,
  matchesShortcut,
  type ShortcutAction,
  type ShortcutPlatform,
} from "./keyboardShortcuts";

/** Delay (ms) before showing badges after modifier is held. */
const SHOW_DELAY_MS = 250;

type TopicShortcutEntry = {
  scope: "global" | "project";
  workspaceRoot: string;
  topicId: string;
  sessionPath?: string;
};

type TopicShortcutKeyboardEvent = Pick<globalThis.KeyboardEvent, "key" | "ctrlKey" | "metaKey" | "altKey" | "shiftKey" | "defaultPrevented" | "target">;

function topicShortcutAction(index: number): ShortcutAction {
  return `topic.goto.${index}` as ShortcutAction;
}

function isPlatformModifierKey(key: string, platform: ShortcutPlatform): boolean {
  return platform === "darwin" ? key === "Meta" : key === "Control";
}

function hasOnlyPlatformModifier(event: TopicShortcutKeyboardEvent, platform: ShortcutPlatform): boolean {
  if (platform === "darwin") return Boolean(event.metaKey) && !event.ctrlKey && !event.altKey && !event.shiftKey;
  return Boolean(event.ctrlKey) && !event.metaKey && !event.altKey && !event.shiftKey;
}

export function topicShortcutLabel(index: number, platform: ShortcutPlatform = detectShortcutPlatform()): string {
  return formatShortcutCombo(defaultShortcutCombo(topicShortcutAction(index), platform), platform);
}

export function topicShortcutIndexFromEvent(
  event: TopicShortcutKeyboardEvent,
  platform: ShortcutPlatform = detectShortcutPlatform(),
): number | null {
  if (event.defaultPrevented) return null;
  // Allow Cmd/Ctrl+1-9 even when focus is in an editable element (input/textarea)
  // because these are application-level navigation shortcuts, not editor keys
  // like Cmd+C/V/Z. Only block during shortcut-recording mode. Matches CodeX
  // behavior where topic shortcuts work regardless of input focus state.
  if (isShortcutRecorderTarget(event.target ?? null)) return null;
  for (let index = 1; index <= 9; index += 1) {
    if (matchesShortcut(event, topicShortcutAction(index), platform)) return index - 1;
  }
  return null;
}

export function useTopicShortcuts(
  enabled = true,
  platform: ShortcutPlatform = detectShortcutPlatform(),
) {
  const [showBadges, setShowBadges] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const heldRef = useRef(false);

  const clearTimer = useCallback(() => {
    if (timerRef.current !== null) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const hideBadges = useCallback(() => {
    clearTimer();
    heldRef.current = false;
    setShowBadges(false);
  }, [clearTimer]);

  useEffect(() => {
    if (!enabled) hideBadges();
  }, [enabled, hideBadges]);

  useEffect(() => {
    if (!enabled) return undefined;

    const onKeydown = (event: globalThis.KeyboardEvent) => {
      if (!isPlatformModifierKey(event.key, platform)) {
        if (heldRef.current) hideBadges();
        return;
      }
      if (!hasOnlyPlatformModifier(event, platform)) {
        hideBadges();
        return;
      }
      if (heldRef.current) return; // already tracking
      heldRef.current = true;
      clearTimer();
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        setShowBadges(true);
      }, SHOW_DELAY_MS);
    };

    const onKeyup = (event: globalThis.KeyboardEvent) => {
      if (!isPlatformModifierKey(event.key, platform)) return;
      hideBadges();
    };

    // If the window loses focus, hide badges
    const onBlur = () => hideBadges();

    document.addEventListener("keydown", onKeydown);
    document.addEventListener("keyup", onKeyup);
    window.addEventListener("blur", onBlur);
    return () => {
      document.removeEventListener("keydown", onKeydown);
      document.removeEventListener("keyup", onKeyup);
      window.removeEventListener("blur", onBlur);
      hideBadges();
    };
  }, [enabled, platform, clearTimer, hideBadges]);

  return { showBadges, hideBadges };
}

export type { TopicShortcutEntry };
