import { useCallback, useRef, useState } from "react";
import { app } from "./bridge";
import { invalidateProjectTreeTopicLoads, projectTreeFolderKeyForTopic } from "./projectTreeTopic";
import type { ToastContextValue } from "./toast";
import type { ProjectNode } from "./types";

export { projectTreeWithoutTopics } from "./projectTreeTopic";

type TopicPageState = { nextCursor?: string; loading: boolean };

export type ProjectTreeRefreshOptions = {
  reloadTopicKeys?: string[];
  reloadAllTopics?: boolean;
  onReloadStarted?: () => void;
};

export type ProjectTreeRefresh = (options?: ProjectTreeRefreshOptions) => Promise<void>;

export async function reloadProjectTreeTopics(
  projects: ProjectNode[],
  options: ProjectTreeRefreshOptions | undefined,
  load: (project: ProjectNode) => Promise<void>,
): Promise<void> {
  const keys = new Set(options?.reloadTopicKeys ?? []);
  const targets = projects.filter((project) => options?.reloadAllTopics || keys.has(project.key));
  const pendingLoads = targets.map(load);
  if (pendingLoads.length > 0) options?.onReloadStarted?.();
  await Promise.all(pendingLoads);
}

export function enqueueProjectTreeArchive(previous: Promise<void>, work: () => Promise<void>): Promise<void> {
  return previous.catch(() => undefined).then(work);
}

export async function runProjectTreeArchiveJob({
  archive,
  reload,
  finishPending,
  recover,
}: {
  archive: () => Promise<void>;
  reload: () => Promise<void>;
  finishPending: () => void;
  recover: (error: unknown) => Promise<void>;
}): Promise<boolean> {
  try {
    await archive();
  } catch (error) {
    // Failed mutations must become visible to the recovery reload.
    finishPending();
    await recover(error);
    return false;
  }
  try {
    // Keep the visible pending state active until the canonical folder page
    // has landed. The caller may release its stale-response tombstone once
    // that reload has acquired a newer request generation.
    await reload();
    return true;
  } finally {
    finishPending();
  }
}

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
  const tombstonesRef = useRef<Set<string>>(new Set());
  const [topics, setTopics] = useState<Set<string>>(new Set());
  const begin = useCallback((topicId: string) => {
    if (topicsRef.current.has(topicId)) return false;
    topicsRef.current = projectTreeTrashingTopics(topicsRef.current, topicId, true);
    tombstonesRef.current = projectTreeTrashingTopics(tombstonesRef.current, topicId, true);
    setTopics(topicsRef.current);
    return true;
  }, []);
  const end = useCallback((topicId: string) => {
    topicsRef.current = projectTreeTrashingTopics(topicsRef.current, topicId, false);
    tombstonesRef.current = projectTreeTrashingTopics(tombstonesRef.current, topicId, false);
    setTopics(topicsRef.current);
  }, []);
  const releaseTombstone = useCallback((topicId: string) => {
    tombstonesRef.current = projectTreeTrashingTopics(tombstonesRef.current, topicId, false);
  }, []);
  const currentTombstones = useCallback((): ReadonlySet<string> => tombstonesRef.current, []);
  return {
    trashingTopics: topics,
    beginTrashingTopic: begin,
    endTrashingTopic: end,
    releaseArchiveTombstone: releaseTombstone,
    currentArchiveTombstones: currentTombstones,
  };
}

export function useProjectTreeArchiveController({
  treeRef,
  topicLoadSeqRef,
  topicPageStateRef,
  updateTopicPageState,
  refreshRef,
  optimisticallyRemoveTopic,
  closeMenu,
  onTopicsChanged,
  showToast,
}: {
  treeRef: { current: ProjectNode[] };
  topicLoadSeqRef: { current: Record<string, number> };
  topicPageStateRef: { current: Record<string, TopicPageState> };
  updateTopicPageState: (key: string, next: TopicPageState) => void;
  refreshRef: { current: ProjectTreeRefresh };
  optimisticallyRemoveTopic: (topicId: string) => void;
  closeMenu: () => void;
  onTopicsChanged?: () => Promise<void> | void;
  showToast: ToastContextValue["showToast"];
}) {
  const {
    trashingTopics,
    beginTrashingTopic,
    endTrashingTopic,
    releaseArchiveTombstone,
    currentArchiveTombstones,
  } = useProjectTreeArchiveState();
  const archiveQueueRef = useRef<Promise<void>>(Promise.resolve());

  const trashTopic = useCallback(async (topicId: string) => {
    if (!beginTrashingTopic(topicId)) return;
    const folderKey = projectTreeFolderKeyForTopic(treeRef.current, topicId);
    const reloadOptions: ProjectTreeRefreshOptions = {
      reloadTopicKeys: folderKey ? [folderKey] : undefined,
      reloadAllTopics: !folderKey,
      onReloadStarted: () => releaseArchiveTombstone(topicId),
    };
    const invalidatedKeys = folderKey
      ? [folderKey]
      : treeRef.current.filter((node) => node.kind === "project" || node.kind === "global_folder").map((node) => node.key);
    // Retire any request that captured the pre-archive catalog before it can
    // reinsert the optimistic tombstone while the backend mutation is pending.
    invalidateProjectTreeTopicLoads(topicLoadSeqRef.current, invalidatedKeys);
    for (const key of invalidatedKeys) {
      updateTopicPageState(key, { ...topicPageStateRef.current[key], loading: false });
    }
    optimisticallyRemoveTopic(topicId);
    closeMenu();

    const queued = archiveQueueRef.current.catch(() => undefined).then(async () => {
      try {
        await app.TrashTopic(topicId);
      } catch (err) {
        endTrashingTopic(topicId);
        showToast(err instanceof Error ? err.message : String(err), "error");
        await refreshRef.current(reloadOptions);
        return;
      }
      try {
        await refreshRef.current(reloadOptions);
        await Promise.resolve(onTopicsChanged?.()).catch(() => undefined);
      } finally {
        endTrashingTopic(topicId);
      }
    });
    archiveQueueRef.current = queued;
    await queued;
  }, [beginTrashingTopic, closeMenu, endTrashingTopic, onTopicsChanged, optimisticallyRemoveTopic, refreshRef, releaseArchiveTombstone, showToast, topicLoadSeqRef, topicPageStateRef, treeRef, updateTopicPageState]);

  return { trashingTopics, currentArchiveTombstones, trashTopic };
}
