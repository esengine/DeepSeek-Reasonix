import { browserMockScenarioParam, GUIDANCE_QUEUE_MOCK_ITEMS, isGuidanceMockScenario } from "../lib/mockScenarios";
import { formatShortcutCombo, resolvedShortcutCombo } from "../lib/keyboardShortcuts";
import { showWorktreeCleanupNotice } from "../lib/worktreeCleanupNotice";
import { desktopBridge } from "./desktopBridgeAdapter";
import { desktopProjectAdapter } from "./desktopProjectAdapter";
import { useHistoryCommands } from "./useHistoryCommands";
import { useSessionNavigationCommands } from "./useSessionNavigationCommands";
import { usePaletteCommands } from "./usePaletteCommands";
import { useTopicNavigationShortcuts } from "./useTopicNavigationShortcuts";
import { useProjectTopicCommands } from "./useProjectTopicCommands";
import { useAppChromeCommands } from "./useAppChromeCommands";
import { useOnboardingCommands } from "./useOnboardingCommands";
import { useWorktreeMergeCommands } from "./useWorktreeMergeCommands";
import type { HistoryViewState } from "./historyViewProjection";
import type { State } from "../lib/useController";
import type { TabMeta } from "../lib/types";
import type { Translator } from "../lib/i18n";
import type { useAppRuntimeAdapter } from "./useAppRuntimeAdapter";
import type { useAppShellStores } from "./useAppShellStores";
import type { useNavigationSurface } from "../lib/useNavigationSurface";
import type { useAppSessionComposition } from "./useAppSessionComposition";

type Runtime = ReturnType<typeof useAppRuntimeAdapter>;
type Shell = ReturnType<typeof useAppShellStores>;
type SessionComposition = ReturnType<typeof useAppSessionComposition>;

export type AppNavigationCompositionInput = {
  runtime: Runtime;
  t: Translator;
  notice: Runtime["snapshot"]["notice"];
  showToast: (message: string, level?: "info" | "warn" | "error", options?: { durationMs?: number }) => void;
  shell: Shell;
  state: State;
  activeTab: TabMeta | undefined;
  activeTabId: string | undefined;
  activeSessionIdentity: string;
  remoteSurfaceActive: boolean;
  surface: Pick<ReturnType<typeof useNavigationSurface>, "begin" | "maskTarget">;
  local: {
    setHistView: React.Dispatch<React.SetStateAction<HistoryViewState | null>>;
    setProjectRevision: React.Dispatch<React.SetStateAction<number>>;
    setSidebarImDetailConnectionId: React.Dispatch<React.SetStateAction<string>>;
    setTasksOpen: React.Dispatch<React.SetStateAction<false | "session" | "all">>;
  };
  session: SessionComposition;
};

/**
 * Navigation/chrome composition: history, automation, desktop and session
 * navigation, command palette, topic shortcuts, project/topic commands,
 * window chrome and worktree merge. Runs after the session composition in
 * the App body's original order; pure relocation.
 */
export function useAppNavigationComposition(input: AppNavigationCompositionInput) {
  const { runtime, t, notice, showToast, shell, state, activeTab, activeTabId, activeSessionIdentity, session } = input;
  const { remoteSurfaceActive } = input;
  const { begin: beginNavigationSurface, maskTarget: settleNavigationSurface } = input.surface;
  const {
    setHistView, setProjectRevision,
    setSidebarImDetailConnectionId, setTasksOpen,
  } = input.local;
  const {
    noteNavigationIntent, registeredNavigationIntent,
    isNavigationIntentCurrent, syncActiveTab, ensureBlankTab, ensureBlankSurface,
  } = runtime.navigation;
  const { listSessions, deleteSession, renameSession } = runtime.sessionActions;
  const { refreshMeta, pickWorkspace, switchWorkspace } = runtime.workspace;
  const {
    managementActive, desktopPlatform, windowsFramelessChrome, singleSurfaceLayout, sidebarCollapsed,
    openPage, returnToWorkspace, enterConversation,
    setSettingsTarget, setSettingsFocus, setSidebarSearchOpen, setSidebarSearchFocusSignal, setProviderSetupNeeded,
  } = shell;
  const { reload: reloadDesktopPreferences } = shell.preferences;
  const {
    closeTransientOverlays, refreshProviderSetupState,
    tabBarCommands, terminalPanelCommands, remoteWorkspaceCommands, runtimeEventCommands,
    desktopNavigation: desktopNavigationBag,
  } = session;
  const { refreshTabMetas, seedActiveTabMeta } = runtimeEventCommands;
  const { handleTabClose } = tabBarCommands;
  const { toggleTerminalPanel } = terminalPanelCommands;
  const { openRemoteWorkspaceFromStatus, connectAndOpenRemoteWorkspace } = remoteWorkspaceCommands;
  const { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject } = desktopNavigationBag;
  const { toggleSidebar } = session.shellGeometry;

  const historyCommands = useHistoryCommands({
    running: state.running,
    setHistView,
    ports: {
      listSessions,
      deleteSession,
      renameSession,
      openPage: (page) => openPage(page),
    },
  });
  const { openTrash, refreshHistoryView } = historyCommands;


  const navigationCommands = useSessionNavigationCommands({
    activeTab,
    running: state.running,
    singleSurface: singleSurfaceLayout,
    t,
    showToast,
    closeTransientOverlays,
    clearImDetail: () => setSidebarImDetailConnectionId(""),
    navigation: { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject },
    noteNavigationIntent,
    beginNavigationSurface,
    settleNavigationSurface,
    isNavigationIntentCurrent,
    markProjectChanged: setProjectRevision,
    refreshTabMetas,
    refreshHistoryView,
    enterConversation,
    pickWorkspace,
    switchWorkspace,
    ports: {
      openTaskSessionForTab: (tabId, taskId) => desktopBridge.openTaskSessionForTab(tabId, taskId),
      listSessionsForTab: (tabId) => desktopBridge.listSessionsForTab(tabId),
    },
  });
  const { openBlankSession, handleNewTab, onResumeSession, switchFolder, handleNavigateTopic } = navigationCommands;

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up; extension actions follow the same snapshot rule.
  const { openPalette, paletteItems } = usePaletteCommands({
    managementActive,
    activeTabId,
    remoteSurfaceActive,
    t,
    notice,
    showToast,
    ports: {
      handleNewTab: () => void handleNewTab(),
      listSessions,
      openTrash: () => void openTrash(),
      onResumeSession: (session) => onResumeSession(session),
      openRemoteWorkspaceFromStatus: (host) => openRemoteWorkspaceFromStatus(host),
      connectAndOpenRemoteWorkspace: (host) => connectAndOpenRemoteWorkspace(host),
      toggleTerminalPanel,
      setTasksOpen: (open) => setTasksOpen(open),
      handleTabClose: (id) => void handleTabClose(id),
      toggleSidebar,
      returnToWorkspace,
    },
  });

  // --- Topic shortcut navigation (Cmd/Ctrl+1-9) ---
  const { showBadges: showTopicBadges, setVisibleTopics: handleVisibleTopicsChange } = useTopicNavigationShortcuts({
    enabled: !sidebarCollapsed && !managementActive,
    platform: desktopPlatform,
    onNavigate: handleNavigateTopic,
  });

  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const projectTopicCommands = useProjectTopicCommands({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    topic: activeTab?.remote ? {
      id: activeTab.id, title: activeTab.topicTitle || "",
      target: { kind: "remote", ...activeTab.remote, sessionPath: activeTab.sessionPath || "" },
    } : activeTab?.topicId ? {
      id: activeTab.topicId, title: activeTab.topicTitle || "", target: { kind: "local", topicId: activeTab.topicId },
    } : undefined,
    ports: { ...desktopProjectAdapter, markChanged: setProjectRevision, refreshTabs: refreshTabMetas, syncActive: syncActiveTab },
    navigation: { openBlank: openBlankSession, enqueue: enqueueNavigation, switchFolder },
    reportError: error => showToast(error instanceof Error ? error.message : String(error), "error"),
  });

  const sidebarExpandBlocked = false;
  const sidebarToggleTitle = sidebarCollapsed
      ? t("sidebar.expand")
      : t("sidebar.collapse");
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const browserMockScenario = browserPreviewChrome ? browserMockScenarioParam() : "";
  const guidanceQueueMockItems = isGuidanceMockScenario(browserMockScenario) ? GUIDANCE_QUEUE_MOCK_ITEMS : undefined;
  // Command palette shortcut label (⌘K / Ctrl+K), platform-aware.
  const commandPaletteShortcut = formatShortcutCombo(
    resolvedShortcutCombo("commandPalette.open", desktopPlatform),
    desktopPlatform,
  );
  const chromeCommands = useAppChromeCommands({
    platform: desktopPlatform,
    windowsFrameless: windowsFramelessChrome,
    closeTransientOverlays,
    clearImDetail: () => setSidebarImDetailConnectionId(""),
    setSettingsFocus,
    setSettingsTarget,
    setSidebarSearchOpen,
    setSidebarSearchFocusSignal,
    refreshMeta,
    refreshProviderSetupState,
    reloadDesktopPreferences: (settings) => reloadDesktopPreferences(settings),
  });
  const onboardingCommands = useOnboardingCommands(() => setProviderSetupNeeded(false));
  const worktreeMergeCommands = useWorktreeMergeCommands({
    singleSurfaceLayout, noteNavigationIntent,
    registeredNavigationIntent, isNavigationIntentCurrent, ensureBlankSurface, ensureBlankTab,
    seedSource: seedActiveTabMeta, listTabs: desktopBridge.listTabs,
    closeWorktree: desktopBridge.closeMergedWorktreeTab, finalize: desktopBridge.finalizeWorktreeMerge,
    showToast, t, showCleanup: (cleanup, translate) => showWorktreeCleanupNotice(cleanup, translate, showToast),
  });
  return {
    historyCommands,
    navigationCommands,
    paletteCommands: { openPalette, paletteItems },
    topicShortcuts: { showTopicBadges, handleVisibleTopicsChange },
    projectTopicCommands,
    chromeCommands,
    onboardingCommands,
    worktreeMergeCommands,
    sidebarExpandBlocked,
    sidebarToggleTitle,
    browserPreviewChrome,
    commandPaletteShortcut,
    guidanceQueueMockItems,
  };
}
