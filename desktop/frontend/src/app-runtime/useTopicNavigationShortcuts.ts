import { useEffect, useRef } from "react";
import { topicShortcutIndexFromEvent, useTopicShortcuts, type TopicShortcutEntry } from "../lib/topicShortcuts";
import type { ShortcutPlatform } from "../lib/keyboardShortcuts";
import { useCommittedCommand } from "../lib/useCommittedCommand";

export function useTopicNavigationShortcuts(input: {
  enabled: boolean;
  platform: ShortcutPlatform;
  onNavigate: (entry: TopicShortcutEntry) => void;
}) {
  const topicsRef = useRef<readonly TopicShortcutEntry[]>([]);
  const onNavigate = useCommittedCommand(input.onNavigate);
  const { showBadges } = useTopicShortcuts(input.enabled, input.platform);
  useEffect(() => {
    if (!input.enabled) return;
    const onKeydown = (event: globalThis.KeyboardEvent) => {
      const index = topicShortcutIndexFromEvent(event, input.platform);
      if (index === null || index >= topicsRef.current.length) return;
      event.preventDefault();
      onNavigate(topicsRef.current[index]);
    };
    document.addEventListener("keydown", onKeydown);
    return () => document.removeEventListener("keydown", onKeydown);
  }, [input.enabled, input.platform, onNavigate]);
  return {
    showBadges,
    setVisibleTopics: useCommittedCommand((topics: TopicShortcutEntry[]) => { topicsRef.current = topics; }),
  };
}
