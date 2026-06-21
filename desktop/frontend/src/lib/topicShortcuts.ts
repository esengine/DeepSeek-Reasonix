// useTopicShortcuts — Cmd hold detection + Cmd+1-10 navigation for sidebar topics.
//
// When the user holds Cmd (macOS) or Ctrl (Windows/Linux) for a brief moment
// without pressing another key, shortcut badges (⌘1 … ⌘0) appear over the
// sidebar topic list. Releasing the modifier hides them immediately. Pressing
// Cmd+1-10 while the badges are visible navigates to the matching topic.

import { useCallback, useEffect, useRef, useState } from "react";
import type { ProjectNode } from "./types";
import { asArray } from "./array";

export const MAX_SHORTCUT_TOPICS = 10;

/** Delay (ms) before showing badges after modifier is held. */
const SHOW_DELAY_MS = 250;

type TopicShortcutEntry = {
  scope: "global" | "project";
  workspaceRoot: string;
  topicId: string;
  sessionPath?: string;
};

/**
 * Flatten the visible topic tree into an ordered list (breadth-first) so we
 * can assign shortcut numbers 1-10 matching visual order.
 */
export function flattenVisibleTopics(nodes: ProjectNode[]): TopicShortcutEntry[] {
  const result: TopicShortcutEntry[] = [];
  const queue: ProjectNode[] = [...nodes];
  while (queue.length > 0 && result.length < MAX_SHORTCUT_TOPICS) {
    const node = queue.shift();
    if (!node) continue;
    const isTopic = node.kind === "topic" || node.kind === "global_topic";
    const isSession = node.kind === "session" || node.kind === "global_session";
    if (isTopic || isSession) {
      const scope = node.kind === "global_topic" || node.kind === "global_session" ? "global" : "project";
      result.push({
        scope,
        workspaceRoot: scope === "global" ? "" : node.root ?? "",
        topicId: node.topicId ?? "",
        sessionPath: node.sessionPath,
      });
    }
    // Enqueue children (folders and their contents)
    const children = asArray(node.children);
    for (const child of children) {
      if (child) queue.push(child);
    }
  }
  return result;
}

export function useTopicShortcuts(
  enabled = true,
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
    if (!enabled) return;

    const isModifier = (key: string) => key === "Meta" || key === "Control";

    const onKeydown = (event: globalThis.KeyboardEvent) => {
      if (!isModifier(event.key)) return;
      // Ignore if focus is in an editable element
      const target = event.target;
      if (target instanceof HTMLElement) {
        const tag = target.tagName.toLowerCase();
        if (target.isContentEditable || tag === "input" || tag === "textarea" || tag === "select") return;
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
      if (!isModifier(event.key)) return;
      hideBadges();
    };

    // If the window loses focus, hide badges
    const onBlur = () => hideBadges();

    document.addEventListener("keydown", onKeydown, { capture: true });
    document.addEventListener("keyup", onKeyup, { capture: true });
    window.addEventListener("blur", onBlur);
    return () => {
      document.removeEventListener("keydown", onKeydown, { capture: true });
      document.removeEventListener("keyup", onKeyup, { capture: true });
      window.removeEventListener("blur", onBlur);
      clearTimer();
    };
  }, [enabled, clearTimer, hideBadges]);

  return { showBadges, hideBadges };
}

export type { TopicShortcutEntry };
