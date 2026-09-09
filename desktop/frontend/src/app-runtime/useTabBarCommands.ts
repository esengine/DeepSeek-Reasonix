import { useRef, useState, type Dispatch, type SetStateAction } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { guardBackendNavigationResult } from "../lib/navigationSurfaceTransition";
import { enqueueNavigationRequest, type PendingNavigationRequest } from "../lib/openTopicCoalescing";
import { useOverlayStore } from "../store/overlays";
import type { ActiveWorkView, TabMeta } from "../lib/types";
import type { ComposerProfile } from "../lib/composerProfile";
import type { Translator } from "../lib/i18n";

export type TabClosePolicy = "keep_running" | "stop_and_close";

export type TabBarCommandsInput = {
  activeTabId: string | undefined;
  tabMetas: readonly TabMeta[];
  deliveryWorktreeRoot: string | undefined;
  t: Translator;
  showToast(message: string, level: "error", options?: { durationMs?: number }): void;
  setTabMetas: Dispatch<SetStateAction<TabMeta[]>>;
  setTabOrderIds: Dispatch<SetStateAction<string[]>>;
  setComposerProfilesByTab: Dispatch<SetStateAction<Record<string, ComposerProfile>>>;
  setTabRevealSignal: Dispatch<SetStateAction<number>>;
  clearWorkspaceConflict(): void;
  ports: {
    closeTab(id: string, policy: TabClosePolicy): Promise<boolean>;
    reorderTabs(ids: string[]): Promise<void>;
    switchTab(id: string, tab?: TabMeta, seq?: number): Promise<unknown>;
    switchRemoteTab(tab: TabMeta, seq?: number): Promise<unknown>;
    refreshTabMetas(apply?: () => boolean, options?: { afterMutation?: boolean }): Promise<TabMeta[]>;
    refreshBackgroundRuntimes(): Promise<void>;
    cancelActive(): void;
    noteNavigationIntent(): number;
    beginNavigationSurface(seq: number): void;
    settleNavigationSurface(seq: number): void;
    isNavigationIntentCurrent(seq: number): boolean;
    reassertVisibleTabAfterStaleNavigation(kind: string, staleTabId: string): Promise<void>;
    enterChatView(): void;
    createIsolatedWorktree(root: string, seq: number): Promise<unknown>;
  };
};

/**
 * Owns the tab-bar commands (change/close/bulk-close/reorder with active-work
 * gates), the single-flight tab switch queue, the background-runtime reveals
 * and the delivery-worktree continuation. Tab close prompts and reveal
 * navigation share one navigation-intent/surface lifecycle; only the visible
 * tab list, reveal signal and close prompt stay on the caller's stores.
 */
export function useTabBarCommands(input: TabBarCommandsInput) {
  const { activeTabId, t, showToast, ports } = input;
  const [pendingClose, setPendingClose] = useState<{ tabId: string; work: ActiveWorkView; stopping: boolean } | null>(null);
  // Tab switches serialize through one queue so a slow switch cannot land
  // events/hydration on the wrong session; switchTab's own load is already
  // seq-guarded, this serializes the backend activation around it.
  const tabSwitchSeqRef = useRef(0);
  const tabSwitchRunningRef = useRef(false);
  const tabSwitchPendingRef = useRef<PendingNavigationRequest<{ tabId: string; optimisticTab?: TabMeta; navigationIntentSeq: number }> | null>(null);
  const setTransientOverlayDismissSignal = useOverlayStore((state) => state.setTransientOverlayDismissSignal);

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });

  const enterChatViewForTabNavigation = useCommittedCommand(() => {
    ports.enterChatView();
  });

  const enqueueTabSwitch = useCommittedCommand((tabId: string, optimisticTab?: TabMeta): Promise<void> => {
    enterChatViewForTabNavigation();
    // Claim the shared navigation epoch at click time, before this request
    // can wait behind an older tab switch. That immediately invalidates any
    // in-flight blank/topic completion from a previous user intent.
    const navigationIntentSeq = ports.noteNavigationIntent();
    ports.beginNavigationSurface(navigationIntentSeq);
    return enqueueNavigationRequest(
      { seqRef: tabSwitchSeqRef, runningRef: tabSwitchRunningRef, pendingRef: tabSwitchPendingRef },
      { tabId, optimisticTab, navigationIntentSeq },
      async (request) => {
        try {
          if (!ports.isNavigationIntentCurrent(request.navigationIntentSeq)) return;
          if (request.optimisticTab?.remote) await ports.switchRemoteTab(request.optimisticTab, request.navigationIntentSeq);
          else await ports.switchTab(request.tabId, request.optimisticTab, request.navigationIntentSeq);
          if (!ports.isNavigationIntentCurrent(request.navigationIntentSeq)) return;
          await ports.refreshTabMetas(
            () => ports.isNavigationIntentCurrent(request.navigationIntentSeq),
            { afterMutation: true },
          );
        } finally {
          ports.settleNavigationSurface(request.navigationIntentSeq);
        }
      },
    );
  });

  const revealBackgroundRuntime = useCommittedCommand(async (tabId: string): Promise<void> => {
    enterChatViewForTabNavigation();
    const navigationIntentSeq = ports.noteNavigationIntent();
    ports.beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealBackgroundRuntime(tabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-background",
        isIntentCurrent: ports.isNavigationIntentCurrent,
        reassert: ports.reassertVisibleTabAfterStaleNavigation,
      })) return;
      await ports.switchTab(meta.id, meta, navigationIntentSeq);
      if (!ports.isNavigationIntentCurrent(navigationIntentSeq)) return;
      await ports.refreshTabMetas(
        () => ports.isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (ports.isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      ports.settleNavigationSurface(navigationIntentSeq);
    }
  });

  const handleTabChange = useCommittedCommand((id: string) => {
    closeTransientOverlays();
    const selected = input.tabMetas.find((tab) => tab.id === id);
    input.setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === id })));
    void enqueueTabSwitch(id, selected);
    input.setTabRevealSignal((signal) => signal + 1);
  });

  const finishTabClose = useCommittedCommand(async (
    id: string,
    policy: TabClosePolicy,
  ): Promise<boolean> => {
    closeTransientOverlays();
    const closed = await ports.closeTab(id, policy);
    if (!closed) {
      showToast(t("runtime.closeFailed"), "error");
      return false;
    }
    input.setComposerProfilesByTab((current) => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    });
    input.setTabMetas((current) => {
      if (current.length <= 1) return current;
      const closingIndex = current.findIndex((tab) => tab.id === id);
      if (closingIndex < 0) return current;
      const closingTab = current[closingIndex];
      const remaining = current.filter((tab) => tab.id !== id);
      if (!closingTab.active && closingTab.id !== activeTabId) return remaining;
      const nextIndex = Math.min(closingIndex, remaining.length - 1);
      const nextActiveId = remaining[nextIndex]?.id;
      return remaining.map((tab) => ({ ...tab, active: tab.id === nextActiveId }));
    });
    await ports.refreshTabMetas(undefined, { afterMutation: true });
    await ports.refreshBackgroundRuntimes();
    input.setTabRevealSignal((signal) => signal + 1);
    return true;
  });

  const handleTabClose = useCommittedCommand(async (id: string) => {
    try {
      const work = await app.ActiveWorkForTab(id);
      if (work.running || work.pendingPrompt || work.jobs.length > 0) {
        setPendingClose({ tabId: id, work, stopping: false });
        return;
      }
    } catch {
      // CloseTabWithPolicy re-checks the controller state atomically.
    }
    await finishTabClose(id, "stop_and_close");
  });

  const resolvePendingClose = useCommittedCommand(async (policy: TabClosePolicy) => {
    const request = pendingClose;
    if (!request || request.stopping) return;
    if (policy === "stop_and_close") setPendingClose({ ...request, stopping: true });
    const closed = await finishTabClose(request.tabId, policy);
    if (closed) setPendingClose(null);
    else setPendingClose((current) => current?.tabId === request.tabId ? { ...current, stopping: false } : current);
  });

  const revealWorkspaceWriter = useCommittedCommand(async () => {
    if (!activeTabId) return;
    enterChatViewForTabNavigation();
    const navigationIntentSeq = ports.noteNavigationIntent();
    ports.beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealWorkspaceWriterForTab(activeTabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-workspace-writer",
        isIntentCurrent: ports.isNavigationIntentCurrent,
        reassert: ports.reassertVisibleTabAfterStaleNavigation,
      })) return;
      input.clearWorkspaceConflict();
      await ports.switchTab(meta.id, meta, navigationIntentSeq);
      if (!ports.isNavigationIntentCurrent(navigationIntentSeq)) return;
      await ports.refreshTabMetas(
        () => ports.isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (ports.isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      ports.settleNavigationSurface(navigationIntentSeq);
    }
  });

  const continueInDeliveryWorktree = useCommittedCommand(async () => {
    const root = input.deliveryWorktreeRoot;
    if (!root) return;
    ports.cancelActive();
    input.clearWorkspaceConflict();
    const navigationIntentSeq = ports.noteNavigationIntent();
    ports.beginNavigationSurface(navigationIntentSeq);
    try {
      await ports.createIsolatedWorktree(root, navigationIntentSeq);
      await ports.refreshTabMetas(undefined, { afterMutation: true });
    } catch (err) {
      if (ports.isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      ports.settleNavigationSurface(navigationIntentSeq);
    }
  });

  const handleTabsClose = useCommittedCommand(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = input.tabMetas.map((tab) => tab.id);
    const targets = ids.filter((id, index) => currentIds.includes(id) && ids.indexOf(id) === index);
    if (targets.length === 0) return;
    for (const id of targets) {
      let work: ActiveWorkView | null = null;
      try {
        work = await app.ActiveWorkForTab(id);
      } catch { /* the close path remains authoritative */ }
      if (work && (work.running || work.pendingPrompt || work.jobs.length > 0)) {
        setPendingClose({ tabId: id, work, stopping: false });
        return;
      }
      await finishTabClose(id, "stop_and_close");
    }
    if (nextActiveTabId && currentIds.includes(nextActiveTabId)) {
      const selected = input.tabMetas.find((tab) => tab.id === nextActiveTabId);
      input.setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === nextActiveTabId })));
      void enqueueTabSwitch(nextActiveTabId, selected);
    }
    await ports.refreshTabMetas(undefined, { afterMutation: true });
    input.setTabRevealSignal((signal) => signal + 1);
  });

  const handleTabsReorder = useCommittedCommand(async (ids: string[]) => {
    input.setTabOrderIds(ids);
    input.setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await ports.reorderTabs(ids);
    await ports.refreshTabMetas(undefined, { afterMutation: true });
    input.setTabRevealSignal((signal) => signal + 1);
  });

  return {
    pendingClose,
    setPendingClose,
    enqueueTabSwitch,
    revealBackgroundRuntime,
    handleTabChange,
    finishTabClose,
    handleTabClose,
    resolvePendingClose,
    revealWorkspaceWriter,
    continueInDeliveryWorktree,
    handleTabsClose,
    handleTabsReorder,
  };
}
