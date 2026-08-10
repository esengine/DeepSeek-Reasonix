import { useCallback, useRef, useState } from "react";

export function projectTreeTrashingTopics(previous: Set<string>, topicId: string, trashing: boolean): Set<string> {
  const id = topicId.trim();
  if (!id || previous.has(id) === trashing) return previous;
  const next = new Set(previous);
  if (trashing) next.add(id);
  else next.delete(id);
  return next;
}

export function useProjectTreeArchiveState() {
  const topicsRef = useRef<Set<string>>(new Set());
  const [topics, setTopics] = useState<Set<string>>(new Set());
  const begin = useCallback((topicId: string) => {
    if (topicsRef.current.has(topicId)) return false;
    topicsRef.current = projectTreeTrashingTopics(topicsRef.current, topicId, true);
    setTopics(topicsRef.current);
    return true;
  }, []);
  const end = useCallback((topicId: string) => {
    topicsRef.current = projectTreeTrashingTopics(topicsRef.current, topicId, false);
    setTopics(topicsRef.current);
  }, []);
  return { trashingTopics: topics, beginTrashingTopic: begin, endTrashingTopic: end };
}
