import { useCommittedCommand } from "../lib/useCommittedCommand";
import { asArray } from "../lib/array";
import { resolveTaskMonitorSession } from "../lib/taskMonitorNavigation";
import { taskSessionIDFromPath, type SidebarImConnection } from "./sidebarImProjection";
import type { useDesktopNavigation } from "./useDesktopNavigation";
import type { WorkspaceNavigationPorts } from "./navigationOwner";
import type { ControlResult, SessionMeta, TabMeta } from "../lib/types";
import type { TopicShortcutEntry } from "../lib/topicShortcuts";
import type { Translator } from "../lib/i18n";
import type { Dispatch, SetStateAction } from "react";

const loadNavigationOwner = () => import("./navigationOwner");

export type SessionNavigationCommandsInput = {
  activeTab: TabMeta | undefined;
  running: boolean;
  singleSurface: boolean;
  t: Translator;
  showToast: (message: string, level: "error") => void;
  closeTransientOverlays: () => void;
  clearImDetail: () => void;
  navigation: Pick<ReturnType<typeof useDesktopNavigation>, "enqueueNavigation" | "enqueueNavigationWithIntent" | "openRemoteProject">;
  noteNavigationIntent: () => number;
  beginNavigationSurface: (seq: number) => void;
  settleNavigationSurface: (seq: number) => void;
  isNavigationIntentCurrent: (seq: number) => boolean;
  markProjectChanged: Dispatch<SetStateAction<number>>;
  refreshTabMetas: (apply?: () => boolean, options?: { afterMutation?: boolean }) => Promise<unknown>;
  refreshHistoryView: () => void;
  enterConversation: () => void;
  pickWorkspace: WorkspaceNavigationPorts["pickWorkspace"];
  switchWorkspace: WorkspaceNavigationPorts["switchWorkspace"];
  ports: {
    openTaskSessionForTab(tabId: string, taskId: string): Promise<ControlResult>;
    listSessionsForTab(tabId: string): Promise<SessionMeta[]>;
  };
};

/**
 * Owns the session-level navigation commands: blank/topic/resume/sidebar-IM
 * enqueues, new-tab routing (remote hosts reopen remotely), recovery refresh
 * pairs, folder switching through the lazy navigation owner and the
 * task-monitor session lookup with its navigation-intent fence. All commands
 * coalesce through the shared navigation epoch from useDesktopNavigation.
 */
export function useSessionNavigationCommands(input: SessionNavigationCommandsInput) {
  const { activeTab, running, singleSurface, t, showToast, navigation, ports } = input;

  const blankSessionTarget = useCommittedCommand(() => {
    const activeWorkspaceRoot = activeTab?.scope === "project" ? activeTab.workspaceRoot || "" : "";
    const scope = activeWorkspaceRoot ? "project" : "global";
    return { scope, workspaceRoot: activeWorkspaceRoot };
  });

  const openBlankSession = useCommittedCommand((scope: string, workspaceRoot: string): Promise<void> =>
    navigation.enqueueNavigation({ kind: "blank", scope, workspaceRoot: scope === "project" ? workspaceRoot : "" }));

  const handleNewTab = useCommittedCommand(async () => {
    input.closeTransientOverlays();
    input.clearImDetail();
    if (activeTab?.remote) {
      const outcome = await navigation.openRemoteProject(activeTab.remote, { newSession: true });
      if (outcome.status === "failed") showToast(outcome.error instanceof Error ? outcome.error.message : String(outcome.error), "error");
      return;
    }
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  });

  const handleOpenTopic = useCommittedCommand((scope: string, workspaceRoot: string, topicId: string, sessionPath?: string): Promise<void> => {
    input.closeTransientOverlays();
    input.clearImDetail();
    return navigation.enqueueNavigation({ kind: "topic", scope, workspaceRoot, topicId, sessionPath });
  });

  const openSidebarImConnectionSession = useCommittedCommand((connection: SidebarImConnection): Promise<void> => {
    input.clearImDetail();
    return navigation.enqueueNavigation({ kind: "sidebar-im", connection });
  });

  const onResumeSession = useCommittedCommand((session: SessionMeta): Promise<void> => {
    if (running && !singleSurface) return Promise.resolve();
    return navigation.enqueueNavigation({ kind: "resume-session", session });
  });

  const onRecoveryCreated = useCommittedCommand(() => {
    input.markProjectChanged((value) => value + 1);
    void input.refreshTabMetas(undefined, { afterMutation: true });
  });
  const onRecoveryLineageChanged = useCommittedCommand(() => {
    input.markProjectChanged((value) => value + 1);
    input.refreshHistoryView();
  });

  const openTaskMonitorSession = useCommittedCommand(async (tabID: string, taskID: string): Promise<boolean> => {
    if (running && !singleSurface) {
      throw new Error(t("history.failedOpenSession"));
    }
    // Claim the navigation epoch before the first Wails await. If the user
    // switches tabs while the task/session lookup is pending, its completion is
    // stale and must not enqueue a newer navigation request.
    const navigationIntentSeq = input.noteNavigationIntent();
    input.beginNavigationSurface(navigationIntentSeq);
    let session: SessionMeta | null;
    try {
      session = await resolveTaskMonitorSession({
        tabID,
        taskID,
        intentSeq: navigationIntentSeq,
        isIntentCurrent: input.isNavigationIntentCurrent,
        openTaskSessionForTab: (sourceTabID, sourceTaskID) => ports.openTaskSessionForTab(sourceTabID, sourceTaskID),
        listSessionsForTab: async (sourceTabID) => asArray(await ports.listSessionsForTab(sourceTabID)),
        sessionIDFromPath: taskSessionIDFromPath,
      });
    } catch (error) {
      input.settleNavigationSurface(navigationIntentSeq);
      throw error;
    }
    if (!session) {
      input.settleNavigationSurface(navigationIntentSeq);
      return false;
    }
    await navigation.enqueueNavigationWithIntent({ kind: "resume-session", session }, navigationIntentSeq);
    return input.isNavigationIntentCurrent(navigationIntentSeq);
  });

  const refreshTabsAfterMutation = useCommittedCommand((latest: () => boolean) => (
    input.refreshTabMetas(latest, { afterMutation: true })
  ));
  const switchFolder = useCommittedCommand(async (path?: string) => {
    input.enterConversation();
    return loadNavigationOwner().then(({ navigateWorkspace }) => navigateWorkspace(path, {
      claimIntent: input.noteNavigationIntent,
      beginSurface: input.beginNavigationSurface,
      isIntentCurrent: input.isNavigationIntentCurrent,
      pickWorkspace: input.pickWorkspace,
      switchWorkspace: input.switchWorkspace,
      markProjectChanged: input.markProjectChanged,
      refreshTabsAfterMutation,
      maskTarget: input.settleNavigationSurface,
    }));
  });

  const handleNavigateTopic = useCommittedCommand((entry: TopicShortcutEntry) => {
    void handleOpenTopic(entry.scope, entry.workspaceRoot, entry.topicId, entry.sessionPath);
  });

  return {
    openBlankSession,
    handleNewTab,
    handleOpenTopic,
    openSidebarImConnectionSession,
    onResumeSession,
    onRecoveryCreated,
    onRecoveryLineageChanged,
    openTaskMonitorSession,
    refreshTabsAfterMutation,
    switchFolder,
    handleNavigateTopic,
  };
}
