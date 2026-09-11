import { useState, type Dispatch, type SetStateAction } from "react";
import { app } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { guardBackendNavigationResult } from "../lib/navigationSurfaceTransition";
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
  // setTabOrderIds, ports.reorderTabs and ports.switchRemoteTab lost their only
  // reader with the app tab strip; they stay declared until the caller drops them.
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
 * Owns the tab close command and prompt, the background-runtime reveals and the
 * delivery-worktree continuation. Tab close prompts and reveal navigation share
 * one navigation-intent/surface lifecycle; only the visible tab list, reveal
 * signal and close prompt stay on the caller's stores.
 */
export function useTabBarCommands(input: TabBarCommandsInput) {
  const { activeTabId, t, showToast, ports } = input;
  const [pendingClose, setPendingClose] = useState<{ tabId: string; work: ActiveWorkView; stopping: boolean } | null>(null);
  const setTransientOverlayDismissSignal = useOverlayStore((state) => state.setTransientOverlayDismissSignal);

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });

  const enterChatViewForTabNavigation = useCommittedCommand(() => {
    ports.enterChatView();
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

  return {
    pendingClose,
    setPendingClose,
    revealBackgroundRuntime,
    finishTabClose,
    handleTabClose,
    resolvePendingClose,
    revealWorkspaceWriter,
    continueInDeliveryWorktree,
  };
}
