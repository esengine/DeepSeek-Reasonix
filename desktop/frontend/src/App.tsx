import { lazy, Suspense, useEffect, useLayoutEffect, useMemo, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from "react";
import { useManagementWorkspace } from "./lib/useManagementWorkspace";
import { useAppNavigationStore } from "./store/appNavigation";
import { useAutomationNavigation } from "./app-runtime/useAutomationNavigation";
import { ShellExpandProvider } from "./lib/shellExpand";
import {
  Activity,
  AlarmClock,
  Search,
  Server,
  SquarePen,
  PanelRight,
  Settings as SettingsIcon,
  RotateCw,
  Trash2,
  BarChart3,
  Brain,
  Cpu,
  Palette,
  Puzzle,
  TerminalSquare,
} from "lucide-react";
import { useToast } from "./lib/toast";
import { useGoalActionHandler } from "./lib/goalAction";
import { useWailsResizeFix } from "./lib/useWailsResizeFix";
import { asArray } from "./lib/array";
import { activeLeaseBlockedTab, createBoundedRefreshCoordinator, sameTabMetaLists, seedActiveTabMetaList, shouldRefreshTabMetaForEvent, TAB_META_MAX_IN_FLIGHT } from "./lib/tabMetaRefresh";
import { useT, useI18n } from "./lib/i18n";
import { useActiveRemoteSession } from "./lib/useRemoteSession";
import { useRemoteTabOpened } from "./lib/useRemoteTabOpened";
import { publishNavigationIntent } from "./lib/useNavigationIntentFence";
import { useProjectTopicCommands } from "./app-runtime/useProjectTopicCommands";
import { desktopProjectAdapter } from "./app-runtime/desktopProjectAdapter";
import { projectConversation, projectConversationLayout, projectNavigationSurfaceTarget } from "./app-runtime/conversationProjection";
import { useWorkspacePanelCommands } from "./app-runtime/useWorkspacePanelCommands";
import { useDesktopNavigation } from "./app-runtime/useDesktopNavigation";
import { useRuntimeStatus } from "./app-runtime/useRuntimeStatus";
import { sessionsForScope, type HistoryViewState } from "./app-runtime/historyViewProjection";
import { useTerminalPanelCommands } from "./app-runtime/useTerminalPanelCommands";
import { type HistoryLoadTrigger, type Item } from "./lib/useController";
import { app, onProjectTreeChanged, openExternal } from "./lib/bridge";
import { useDesktopPreferences } from "./app-runtime/useDesktopPreferences";
import { clearAttentionChimeKeys, playAttentionChime, playSuccessChime, shouldPlayAttentionChimeForEvent } from "./lib/sound";
import { Transcript } from "./components/Transcript";
import { decisionSurfaceMockFromInput, type DecisionSurfaceKind as MockDecisionSurfaceKind } from "./lib/decisionSurfaceMock";
const RemoteSessionSurface = lazy(() => import("./components/RemoteSessionSurface").then((module) => ({ default: module.RemoteSessionSurface })));
const SidebarImConnectionDetail = lazy(() => import("./app-shell/SidebarImConnectionDetail").then((module) => ({ default: module.SidebarImConnectionDetail })));
const WindowsWindowControls = lazy(() => import("./app-shell/WindowsWindowControls").then((module) => ({ default: module.WindowsWindowControls })));
import { DecisionFooterRegion, type DecisionFooterSurface } from "./app-shell/DecisionFooterRegion";
/** Footer decision surface kinds. Runtime blockers are explicit recovery choices. */
type DecisionSurfaceKind = MockDecisionSurfaceKind | "extension_form";
import { RemoteConnectionTimeoutError, useRemoteStore, waitForRemoteConnection } from "./store/remote";
import { RemoteWorkspaceLaunchGate, resolveRemoteWorkspace } from "./lib/remoteWorkspace";
import type { PaletteItem } from "./components/CommandPalette";
import { UpdaterProvider } from "./lib/useUpdater";
import { Tooltip } from "./components/Tooltip";
import { useOnboardingCommands } from "./app-runtime/useOnboardingCommands";
import { AppChrome } from "./components/AppChrome";
import { TopicbarRegion } from "./app-shell/TopicbarRegion";
import { shouldMountExternalOpener } from "./components/ExternalOpener";
import { TopicbarActionsRegion } from "./app-shell/TopicbarActionsRegion";
import { formatTerminalOutputForComposer } from "./lib/terminalOutput";
import { setReasoningDisplayPending } from "./lib/reasoningDisplayPreference";
import { parseTodos } from "./lib/tools";
import {
  dismissedTodoKeyForScope,
  resolveTodoPanelTodos,
  scopedTodoBatchKey,
  scopedTodoDismissalKey,
  shouldShowTodoPanel,
  todoBatchKey,
  todoContinueTarget,
  todoDismissalKey,
  todoPanelScope,
} from "./lib/todoVisibility";
import {
  type ActiveWorkView,
  type CollaborationMode,
  type ComposerInsertRequest,
  type RewindResultView,
  type RemoteHostView,
  type SessionMeta,
  type SettingsView,
  type QualityFloor,
  type TabMeta,
  type ToolApprovalMode,
  type WireCompletionSummary,
  type WorktreeMergeResult,
} from "./lib/types";
import { runWorktreeMergeLifecycle } from "./lib/worktreeMergeLifecycle";
import { showWorktreeCleanupNotice } from "./lib/worktreeCleanupNotice";
import { requestSessionVersions } from "./lib/sessionRecoveryVersionHostBridge";
import type { WorkspaceVerificationRevealRequest } from "./components/WorkspacePanel";
import type { InvocationMetadataMap, StructuredInvocationSubmit } from "./lib/invocationDisplay";
import type { RewindUndoState } from "./lib/rewindTypes";
import { formatSelectionReference, type SelectedTextInsertRequest } from "./lib/selectedTextContext";
import { resolveTaskMonitorSession } from "./lib/taskMonitorNavigation";
import {
  composerProfileFromMeta,
  composerProfileFromTab,
  composerProfileMode,
  defaultComposerProfile,
  displayedComposerProfileCollaborationMode,
  hydrateComposerProfileFromMeta,
  hydrateComposerProfilesFromTabs,
  patchComposerProfile,
  pruneUserPlanModeIntents,
  resolvePlanRestoreTabId,
  shouldRestoreUserPlanModeForProfile,
  updateUserPlanModeIntent,
  type ComposerProfile,
  type ComposerProfileField,
  type UserPlanModeIntents,
} from "./lib/composerProfile";
import {
  toggleYoloToolApprovalMode,
  restorableToolApprovalMode,
  type RestorableToolApprovalMode,
} from "./lib/toolApprovalMode";
import { useComposerModeActions } from "./lib/useComposerModeActions";
import { useSessionOperations } from "./app-runtime/useSessionOperations";
import { useComposerGoalCommands } from "./app-runtime/useComposerGoalCommands";
import { useRemoteComposerProfileSync, useRemoteComposerRuntimeActions, useRemoteComposerSend } from "./lib/useRemoteComposerIntegration";
import { RemoteNavigationContext, type RemoteNavigationCommand } from "./lib/remoteNavigationCommands";
import {
  SIDEBAR_MAX_WIDTH,
  TERMINAL_DEFAULT_HEIGHT,
  TERMINAL_MIN_HEIGHT,
  defaultCreationRightDockTreeWidth,
  defaultCreationSidebarWidth,
  defaultRightDockTreeWidth,
  defaultSidebarWidth,
  useLayoutStore,
} from "./store/layout";
import { useOverlayStore } from "./store/overlays";
import { recordFrontendDiagnostic } from "./lib/frontendDiagnosticBridge";
import { paletteSessionDisplayTitle, paletteSessionHint, paletteSessionKeywords, sessionActivityTime } from "./lib/session";
import { enqueueNavigationRequest, type PendingNavigationRequest } from "./lib/openTopicCoalescing";
import {
  guardBackendNavigationResult,
} from "./lib/navigationSurfaceTransition";
import { useNavigationSurface } from "./lib/useNavigationSurface";
import {
  applyTheme,
  getTheme,
  getThemeStyle,
  isThemeStyle,
  type Theme,
} from "./lib/theme";
import { applyThemeScene, clearThemePack } from "./lib/themePack";
import { ThemeBackground } from "./components/ThemeBackground";
import { tabWorkspaceTitle, topicDisplayTitle, topicTitle, safeFilename } from "./lib/sessionTitles";
import { GUIDANCE_QUEUE_MOCK_ITEMS, browserMockScenarioParam, isGuidanceMockScenario } from "./lib/mockScenarios";
import { loadDismissedTodoKeys, saveDismissedTodoKeys } from "./lib/todoDismissalStorage";
import { NoticePreviewPanel, noticePreviewMockEnabled } from "./app-shell/NoticePreviewPanel";
import { ShellHotkeys, TextSizeHotkeys } from "./app-shell/HotkeyRegistrations";
import { useViewportHeightVar, useWindowStatePersistence } from "./lib/windowState";
import { workspacePanelAriaMinWidth } from "./lib/workspaceLayout";
import { formatShortcutCombo, resolvedShortcutCombo, useGlobalShortcut } from "./lib/keyboardShortcuts";
import { useWarmTerminalPanel } from "./lib/useWarmTerminalPanel";
import { topicShortcutIndexFromEvent, useTopicShortcuts, type TopicShortcutEntry } from "./lib/topicShortcuts";
import { composerDraftKeyForTab } from "./lib/composerDraftKey";
import { useSessionSubmission } from "./lib/useSessionSubmission";
import { usePendingPlanRevisions, reportPendingRevisionFailure } from "./lib/usePendingPlanRevisions";
import { createSubmissionPorts, projectSubmissionResources } from "./app-runtime/desktopSubmissionAdapter";
import { goalCommand } from "./app-runtime/sessionSubmissionOwner";
import { useCommittedCommand } from "./lib/useCommittedCommand";
import { createSessionSurfaceFence, sessionIdentityKey } from "./app-runtime/sessionTarget";
import { commitAppRenderToken, createAppRenderToken } from "./app-runtime/appLifecycleProbe";
import { useAppRuntimeAdapter } from "./app-runtime/useAppRuntimeAdapter";
import { projectControllerProfiles } from "./app-runtime/controllerProfileOwner";
import { useControllerProfileCommands } from "./lib/useControllerProfileCommands";
import { AppOverlayHost } from "./app-shell/AppOverlayHost";
import { WorkspaceDockRegion } from "./app-shell/WorkspaceDockRegion";
import { AppBottomRegions } from "./app-shell/AppBottomRegions";
import { SidebarRegion } from "./app-shell/SidebarRegion";
import { isMacOSWorkbenchSidebarTitlebar } from "./lib/desktopPlatform";
import { useWindowChromeStore } from "./store/windowChrome";
import { WindowChromeLifecycle } from "./app-runtime/WindowChromeLifecycle";
import { StartupGateLifecycle, probeProviderSetupState } from "./app-runtime/StartupGateLifecycle";
import { useSessionBannerCommands } from "./app-runtime/useSessionBannerCommands";
import { SessionStatusBanners } from "./app-shell/SessionStatusBanners";
import { useShellGeometry } from "./app-runtime/useShellGeometry";
import { nativeWindowCommands, useWindowsMaximised } from "./app-runtime/useNativeWindowController";
import {
  sidebarImScopeLabel,
  taskSessionIDFromPath,
  type SidebarImConnection,
} from "./app-runtime/sidebarImProjection";
import {
  AppRuntimeEffects,
  type RemoteForwardsListener,
  type RemoteServerListener,
  type RemoteStatusListener,
  type RuntimeEventListener,
  type RuntimeReadyListener,
  type RuntimeRebuiltListener,
} from "./app-runtime/AppRuntimeEffects";
import { useSessionPromptCommands } from "./app-runtime/useSessionPromptCommands";
// Hold reasoning UI until the authoritative desktop startup settings arrive;
// this prevents a hidden preference from flashing content during first paint.
setReasoningDisplayPending();
const TaskMonitorPanel = lazy(() => import("./components/TaskMonitorPanel").then((module) => ({ default: module.TaskMonitorPanel })));
const loadNavigationOwner = () => import("./app-runtime/navigationOwner");
const loadDeliveryContinue = () => import("./lib/deliveryContinue");
function setRemoteComposerProfileForSessionAction(
  tabId: string,
  mode: CollaborationMode,
  approvalMode: ToolApprovalMode,
  goal: string,
) {
  return app.SetRemoteTabComposerProfile(tabId, mode, approvalMode, goal);
}
function lazyContinueDelivery(options: import("./lib/deliveryContinue").DeliveryContinueOptions) {
  return loadDeliveryContinue().then(({ continueDelivery }) => continueDelivery(options));
}
function lazyNavigateWorkspace(
  path: string | undefined,
  ports: import("./app-runtime/navigationOwner").WorkspaceNavigationPorts,
) {
  return loadNavigationOwner().then(({ navigateWorkspace }) => navigateWorkspace(path, ports));
}

const WORKSPACE_RESIZER_WIDTH = 8;

function isThemeMode(value: string): value is Theme {
  return value === "auto" || value === "light" || value === "dark";
}

const SHOW_CONTEXT_DOCK = true;
type WorkspaceInsertTarget = "composer" | "planRevision";
export default function App() {
  const appRenderToken = createAppRenderToken();
  useLayoutEffect(() => commitAppRenderToken(appRenderToken));
  const runtime = useAppRuntimeAdapter();
  const { state, liveStore, activeTabId, notice } = runtime.snapshot;
  const {
    sendToTab, runShellForTab, steerForTab, cancel, cancelForTab,
    setControllerModeForTab, setCollaborationMode: setControllerCollaborationMode,
    setCollaborationModeForTab: setControllerCollaborationModeForTab,
    setToolApprovalModeForTab, setQualityFloor: setControllerQualityFloor,
    setComposerProfileForTab: setControllerComposerProfileForTab, setGoalForTab: setControllerGoalForTab,
    resumeGoalForTab: resumeControllerGoalForTab, pauseGoalForTab: pauseControllerGoalForTab,
    clearGoalForTab: clearControllerGoalForTab,
    setModelForTab, setEffortForTab, cancelJob,
  } = runtime.composer;
  const {
    recoverDeliveryToTab, approveForTab, isPromptCurrentForTab, resolvePlanDecisionForTab, resolveRecoveryForTab,
    answerQuestionForTab, answerMCPInteractionForTab, dismissExtensionForm, drainExtensionNotifications,
    clearSession, newSession, listSessions, listTrashedSessions, resumeSession, openChannelSession,
    previewSession, deleteSession, restoreSession, purgeTrashedSession, renameSession,
    loadOlderHistory, retrySessionHistory, rewindForTab, rewindForTabDetailed, undoRewindForTab,
  } = runtime.sessionActions;
  const { refreshMeta, pickWorkspace, switchWorkspace } = runtime.workspace;
  const {
    switchTab, switchRemoteTab, openProjectTab, createIsolatedWorktree, openGlobalTab, closeTab,
    reorderTabs, openTopicSession, activateTopic, noteNavigationIntent, registeredNavigationIntent,
    isNavigationIntentCurrent, reassertVisibleTabAfterStaleNavigation, syncActiveTab,
    ensureBlankTab, ensureBlankSurface, commitSingleSurfaceNavigation,
  } = runtime.navigation;
  const t = useT();
  const { locale } = useI18n();
  const { showToast } = useToast();
  const [composerProfilesByTab, setComposerProfilesByTab] = useState<Record<string, ComposerProfile>>({});
  const yoloRestoreToolApprovalModesRef = useRef<Record<string, RestorableToolApprovalMode>>({});
  const userPlanModeByTabRef = useRef<UserPlanModeIntents>({});
  const [tabMetas, setTabMetas] = useState<TabMeta[]>([]);
  const [tabOrderIds, setTabOrderIds] = useState<string[]>([]);
  const activeTab = useMemo(
    () => tabMetas.find((tab) => tab.id === activeTabId) ?? tabMetas.find((tab) => tab.active),
    [activeTabId, tabMetas],
  );
  const { active: remoteSurfaceActive, session: remoteSession, ready: remoteComposerReady, onSend: remoteSend, onCancel: remoteCancel } = useActiveRemoteSession(activeTab, showToast);
  const activeSessionIdentity = sessionIdentityKey({
    tabId: activeTabId,
    sessionPath: activeTab?.sessionPath ?? state.meta?.sessionPath,
    sessionGeneration: activeTab?.sessionGeneration ?? state.meta?.sessionGeneration ?? state.sessionGen,
    scope: activeTab?.scope,
    workspaceRoot: activeTab?.workspaceRoot ?? state.meta?.cwd,
    topicId: activeTab?.topicId,
  });
  const sessionSurfaceFenceRef = useRef<ReturnType<typeof createSessionSurfaceFence> | null>(null);
  if (!sessionSurfaceFenceRef.current) sessionSurfaceFenceRef.current = createSessionSurfaceFence();
  const sessionSurfaceFence = sessionSurfaceFenceRef.current;
  const sessionOperations = useSessionOperations({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    resources: [
      { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
      ...tabMetas.filter(tab => tab.id !== activeTabId).map(tab => ({
        tabId: tab.id,
        sessionKey: sessionIdentityKey({ tabId: tab.id, sessionPath: tab.sessionPath,
          sessionGeneration: tab.sessionGeneration, scope: tab.scope, workspaceRoot: tab.workspaceRoot, topicId: tab.topicId }),
      })),
    ],
  });
  useLayoutEffect(() => {
    sessionSurfaceFence.commit(activeTabId, activeSessionIdentity);
    return () => sessionSurfaceFence.dispose();
  }, [activeSessionIdentity, activeTabId, sessionSurfaceFence]);
  const {
    surface: navigationSurface, transitioning: runtimeTransitioning, dataReady: navigationTargetDataReady,
    preserved: preservedTranscriptSurface, surfaceCommitToken, commitRendered: commitRenderedTranscriptSurface,
    begin: beginNavigationSurface, maskTarget: settleNavigationSurface, commitPaint: commitNavigationSurfacePaint,
  } = useNavigationSurface(projectNavigationSurfaceTarget({
    activeTabId, sessionKey: activeSessionIdentity, local: state, remote: remoteSurfaceActive ? remoteSession : undefined,
  }));
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [transcriptRevealSignal, setTranscriptRevealSignal] = useState(0);
  const startupSplashVisible = useOverlayStore((s) => s.startupSplashVisible);
  const setStartupSplashVisible = useOverlayStore((s) => s.setStartupSplashVisible);
  // null until the mount probe resolves; true shows the first-run guide.
  const needsOnboarding = useOverlayStore((s) => s.needsOnboarding);
  const providerSetupNeeded = useOverlayStore((s) => s.providerSetupNeeded);
  const setProviderSetupNeeded = useOverlayStore((s) => s.setProviderSetupNeeded);
  const page = useAppNavigationStore((s) => s.page);
  const managementActive = page.kind !== "workspace";
  const settingsTarget = page.kind === "settings" ? page.tab : null;
  const openPage = useAppNavigationStore((s) => s.openPage);
  const returnToWorkspace = useAppNavigationStore((s) => s.returnToWorkspace);
  const enterConversation = useAppNavigationStore((s) => s.enterConversation);
  const visitedTrash = useAppNavigationStore((s) => s.visitedTrash);
  const visitedAutomation = useAppNavigationStore((s) => s.visitedAutomation);
  const automationReturn = useAppNavigationStore((s) => s.automationReturn);
  const setSettingsTarget = useAppNavigationStore((s) => s.setSettingsTarget);
  const settingsFocus = useAppNavigationStore((s) => s.settingsFocus);
  const setSettingsFocus = useAppNavigationStore((s) => s.setSettingsFocus);
  const { desktopLayoutStyle, startupUpdateChecksEnabled, statusBarStyle, statusBarItems,
    sidebarImConnections, imTopicSources, configLoadWarnings, reloadConfigWarnings,
    dismissConfigWarnings, reload: reloadDesktopPreferences } = useDesktopPreferences();
  const singleSurfaceLayout = desktopLayoutStyle === "workbench" || desktopLayoutStyle === "creation";
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const paletteOpen = useOverlayStore((s) => s.paletteOpen);
  const setPaletteOpen = useOverlayStore((s) => s.setPaletteOpen);
  const paletteExtensionActions = useOverlayStore((s) => s.paletteExtensionActions);
  const setPaletteExtensionActions = useOverlayStore((s) => s.setPaletteExtensionActions);
  const remoteHosts = useRemoteStore((s) => s.hosts);
  const remoteStatuses = useRemoteStore((s) => s.statuses);
  const { runGoalAction, handleGoalActionError } = useGoalActionHandler();
  const setRemoteHosts = useRemoteStore((s) => s.setHosts);
  const hydrateRemoteStatuses = useRemoteStore((s) => s.hydrateStatuses);
  const requestRemoteExplorer = useRemoteStore((s) => s.openExplorer);
  const applyRemoteStatus = useRemoteStore((s) => s.applyStatus);
  const requestRemoteStatusPopover = useRemoteStore((s) => s.requestStatusPopover);
  const setRemoteForwards = useRemoteStore((s) => s.setForwards);
  const setRemoteServer = useRemoteStore((s) => s.setServer);

  const shortcutsOpen = useOverlayStore((s) => s.shortcutsOpen);
  const setShortcutsOpen = useOverlayStore((s) => s.setShortcutsOpen);
  const paletteSessions = useOverlayStore((s) => s.paletteSessions);
  const setPaletteSessions = useOverlayStore((s) => s.setPaletteSessions);
  const [sidebarImDetailConnectionId, setSidebarImDetailConnectionId] = useState("");
  const sidebarCollapsed = useLayoutStore((s) => s.sidebarCollapsed);
  type TimeFilter = "all" | "10" | "20" | "1h" | "3h" | "5h" | "1d";
  const [topicTimeFilter, setTopicTimeFilter] = useState<TimeFilter>(() => {
    try {
      const saved = localStorage.getItem("projectTree:timeFilter");
      if (saved === "all" || saved === "10" || saved === "20" || saved === "1h" || saved === "3h" || saved === "5h" || saved === "1d") return saved;
    } catch { /* localStorage unavailable */ }
    return "all";
  });
  useEffect(() => {
    try { localStorage.setItem("projectTree:timeFilter", topicTimeFilter); } catch { /* ignore */ }
  }, [topicTimeFilter]);
  const sidebarResizing = useLayoutStore((state) => state.sidebarResizing);
  const [tasksOpen, setTasksOpen] = useState<false | "session" | "all">(false);
  const takeoverDialogTab = useOverlayStore((s) => s.takeoverDialogTab);
  const reclaimBusyTab = useOverlayStore((s) => s.reclaimBusyTab);
  const workspacePanelOpen = useLayoutStore((s) => s.workspacePanelOpen);
  const rightDockTreeWidth = useLayoutStore((s) => s.rightDockTreeWidth);
  const setRightDockTreeWidth = useLayoutStore((s) => s.setRightDockTreeWidth);
  const rightDockPreviewWidth = useLayoutStore((s) => s.rightDockPreviewWidth);
  const attentionChimeEvents = useRef(new Set<string>());
  const workspaceScopeActiveTabRef = useRef(activeTabId);
  const [workspaceControllerEpoch, setWorkspaceControllerEpoch] = useState(0);
  workspaceScopeActiveTabRef.current = activeTabId;
  useEffect(() => {
    recordFrontendDiagnostic("app", "app.surface", {
      hasActiveTab: Boolean(activeTabId),
      tabCount: tabMetas.length,
    });
  }, [activeTabId, tabMetas.length]);

  const workspacePanelResizing = useLayoutStore((state) => state.workspacePanelResizing);
  const liveTerminalHeight = useLayoutStore((state) => state.liveTerminalHeight);
  const setLiveWorkspacePanelRenderWidth = useLayoutStore((state) => state.setLiveWorkspacePanelRenderWidth);
  const terminalResizing = liveTerminalHeight !== null;
  const workspacePanelMaximized = useLayoutStore((s) => s.workspacePanelMaximized);
  const rightDockMode = useLayoutStore((s) => s.rightDockMode);
  const terminalPanelOpen = useLayoutStore((s) => s.terminalPanelOpen);
  const { mounted: terminalContentVisible, fitEnabled: terminalFitEnabled, prefetch: prefetchTerminalPanel } = useWarmTerminalPanel(terminalPanelOpen, terminalResizing, !managementActive);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [fileRefRefreshKey, setFileRefRefreshKey] = useState(0);
  const refreshComposerFileRefs = useCommittedCommand(() => setFileRefRefreshKey((value) => value + 1));
  const composerFileRefRefreshKey = `${dockRefreshKey}:${fileRefRefreshKey}`;
  const [projectRevision, setProjectRevision] = useState(0);
  const [activeTopicTurns, setActiveTopicTurns] = useState<number | undefined>(undefined);
  const [composerInsertRequestsByTab, setComposerInsertRequestsByTab] = useState<Record<string, ComposerInsertRequest>>({});
  const [selectedTextRequestsByTab, setSelectedTextRequestsByTab] = useState<Record<string, SelectedTextInsertRequest>>({});
  const selectedTextRequestIdRef = useRef(0);
  const [planRevisionInsertRequest, setPlanRevisionInsertRequest] = useState<{
    tabId: string;
    approvalId: string;
    request: ComposerInsertRequest;
  } | null>(null);
  const [workspaceInsertTarget, setWorkspaceInsertTarget] = useState<WorkspaceInsertTarget>("composer");
  const transientOverlayDismissSignal = useOverlayStore((s) => s.transientOverlayDismissSignal);
  const setTransientOverlayDismissSignal = useOverlayStore((s) => s.setTransientOverlayDismissSignal);
  const desktopPlatform = useWindowChromeStore((state) => state.platform);
  const windowsFramelessChrome = desktopPlatform === "windows";
  const [mainWindowMaximised, syncMainWindowMaximised] = useWindowsMaximised(windowsFramelessChrome);
  useWailsResizeFix(windowsFramelessChrome, mainWindowMaximised);
  const topicExportOpen = useOverlayStore((s) => s.topicExportOpen);
  const setTopicExportOpen = useOverlayStore((s) => s.setTopicExportOpen);
  const sidebarSearchOpen = useOverlayStore((s) => s.sidebarSearchOpen);
  const setSidebarSearchOpen = useOverlayStore((s) => s.setSidebarSearchOpen);

  const sidebarSearchFocusSignal = useOverlayStore((s) => s.sidebarSearchFocusSignal);
  const setSidebarSearchFocusSignal = useOverlayStore((s) => s.setSidebarSearchFocusSignal);
  const sidebarTogglePressed = useLayoutStore((state) => state.sidebarTogglePressed);
  const [clearContextPending, setClearContextPending] = useState(false);
  const [pendingClose, setPendingClose] = useState<{ tabId: string; work: ActiveWorkView; stopping: boolean } | null>(null);
  const [worktreeMergeTabId, setWorktreeMergeTabId] = useState<string | null>(null);
  const prevDecisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const decisionSurfaceRef = useRef<DecisionSurfaceKind | null>(null);
  const appRef = useRef<HTMLDivElement>(null);
  const layoutRef = useRef<HTMLDivElement>(null);
  useManagementWorkspace(layoutRef, managementActive);

  // Persist window geometry across launches.
  useWindowStatePersistence();
  useViewportHeightVar();

  const { backgroundRuntimes, workspaceConflict, setWorkspaceConflict, refreshBackgroundRuntimes } = useRuntimeStatus({
    tabId: activeTabId, sessionKey: activeSessionIdentity, running: state.running,
  });

  const closeTransientOverlays = useCommittedCommand(() => {
    setTransientOverlayDismissSignal((signal) => signal + 1);
  });


  const openBotSettings = useCommittedCommand(() => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    setSettingsFocus(null);
    setSettingsTarget("bots");
  });

  const openBotAllowlistSettings = useCommittedCommand((connectionId: string) => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    setSettingsFocus({ target: "bot-allowlist", connectionId });
    setSettingsTarget("bots");
  });

  useEffect(() => {
    setSidebarImDetailConnectionId((current) => {
      if (!current) return "";
      return sidebarImConnections.some((connection) => connection.id === current) ? current : "";
    });
  }, [sidebarImConnections]);

  // Open settings when the native menu item (CmdOrCtrl+,) is activated.
  useEffect(() => {
    if (typeof window === "undefined" || !window.runtime) return;
    return window.runtime.EventsOn("app:open-settings", () => {
      closeTransientOverlays();
      setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
    });
  }, [closeTransientOverlays]);

  const [invocationMetadataByTab, setInvocationMetadataByTab] = useState<Record<string, InvocationMetadataMap>>({});
  const [footerHeight, setFooterHeight] = useState(0);
  const footerHeightRef = useRef(0);
  const footerRef = useRef<HTMLElement>(null);
  const activeTabIdRef = useRef(activeTabId);
  const [rewindStatesByTab, setRewindStatesByTab] = useState<Record<string, RewindUndoState>>({});
  const setRewindStateForTab = useCommittedCommand((tabId: string, nextState: RewindUndoState | null) => {
    if (!tabId) return;
    setRewindStatesByTab(current => {
      if (!nextState && !current[tabId]) return current;
      const next = { ...current };
      if (nextState) next[tabId] = nextState;
      else delete next[tabId];
      return next;
    });
  });
  const handleInvocationMetadataChange = useCommittedCommand((metadata: InvocationMetadataMap) => {
    const sourceTabId = activeTabIdRef.current;
    if (!sourceTabId) return;
    setInvocationMetadataByTab((current) => {
      const previous = current[sourceTabId] ?? {};
      const names = Object.keys(metadata);
      if (names.length === Object.keys(previous).length && names.every((name) => (
        previous[name]?.kind === metadata[name]?.kind && previous[name]?.color === metadata[name]?.color
      ))) return current;
      return { ...current, [sourceTabId]: metadata };
    });
  });
  const {
    toggleSidebar, setExpandedSidebarWidth, startSidebarResize, resizeSidebarWithKeyboard,
    setSavedWorkspacePanelWidth, ensureWorkspacePanelWidth, startWorkspacePanelResize, resizeWorkspacePanelWithKeyboard,
    setSavedTerminalHeight, startTerminalResize, resizeTerminalWithKeyboard,
    rightDockTreeWidthClamp, workspacePanelMinWidth, chatReservedWidth,
    workspacePanelAvailableWidth, workspacePanelRenderWidth, workspacePanelOverlay, workspacePanelRenderable,
    workspacePanelGridOpen, sidebarRenderWidth, sidebarResizeMinWidth,
    terminalRenderHeight, terminalResizeMaxHeight,
  } = useShellGeometry({ appRef, layoutRef });

  // Remote tab became ready: refresh the tab list so the spectator banner
  // (takenOver) renders. The agent:ready event only fires for local tabs;
  // remote tabs publish readiness via remote-tab:<id>:state, which
  const conversationView = projectConversation({ local: state, remote: remoteSurfaceActive ? remoteSession : undefined,
    tab: activeTab, activeTabId, backgroundRuntimes, connectingLabel: t("status.connecting") });
  const visibleRuntimeState = conversationView.runtime;
  const sidebarImDetailConnection = useMemo(
    () => sidebarImConnections.find((connection) => connection.id === sidebarImDetailConnectionId) ?? null,
    [sidebarImConnections, sidebarImDetailConnectionId],
  );
  const chatSurfaceVisible = true;
  const { dockVisible: surfaceWorkspacePanelRenderable, dockGridOpen: surfaceWorkspacePanelGridOpen,
    dockOverlay: surfaceWorkspacePanelOverlay,
    terminalOpen: terminalSurfaceOpen } = projectConversationLayout({
    chatVisible: chatSurfaceVisible, localToolsEnabled: conversationView.localToolsEnabled, dockMode: rightDockMode,
    dockRenderable: workspacePanelRenderable, dockGridOpen: workspacePanelGridOpen, dockOverlay: workspacePanelOverlay,
    dockOpen: workspacePanelOpen, dockMaximized: workspacePanelMaximized, terminalOpen: terminalPanelOpen,
  });
  const statusBarVisible = chatSurfaceVisible && !sidebarImDetailConnection;
  const activePlanRevisionInsertRequest =
    planRevisionInsertRequest &&
    planRevisionInsertRequest.tabId === activeTabId &&
    planRevisionInsertRequest.approvalId === state.approval?.id
      ? planRevisionInsertRequest.request
      : null;
  const composerInsertRequest = activeTabId ? composerInsertRequestsByTab[activeTabId] ?? null : null;
  const handleRevisionActiveChange = useCommittedCommand((active: boolean) => {
    setWorkspaceInsertTarget(active ? "planRevision" : "composer");
  });
  const selectedTextRequest = activeTabId ? selectedTextRequestsByTab[activeTabId] ?? null : null;
  const prefillSubagentCommand = useCommittedCommand((command: string) => {
    if (!activeTabId) return;
    setComposerInsertRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: Date.now(), text: command, mode: "prefix" },
    }));
  });
  const composerSessionKey = useMemo(() => {
    return composerDraftKeyForTab(activeTab, activeTabId);
  }, [activeTab, activeTabId]);
  const transcriptGeometrySessionKey = activeSessionIdentity;
  const workspaceScopeKey = [
    activeTabId ?? "",
    activeTab?.sessionPath ?? "",
    state.meta?.sessionPath ?? "",
    state.meta?.cwd ?? "",
    state.sessionGen,
    workspaceControllerEpoch,
  ].join("\u0000");
  // Workspace navigation belongs to the project, not to a single conversation.
  // A session switch inside the same project must therefore retain the dock,
  // tree and selection state.
  const workspaceTreeMemoryKey = [
    activeTab?.scope ?? "",
    activeTab?.workspaceRoot ?? state.meta?.cwd ?? "",
  ].join("\u0000");
  const restoreWorkspaceDockWidths = useCommittedCommand((treeWidth: number, _previewWidth: number) => {
    // Single-width dock: only the tree width is meaningful; clamp it to the
    // dynamic available width (chat keeps its 400px floor), never a fixed
    // 560 ceiling, so the user's remembered width is preserved when reopened.
    setRightDockTreeWidth(rightDockTreeWidthClamp(treeWidth, workspacePanelAvailableWidth));
  });
  useEffect(() => {
    let cancelled = false;
    if (!activeTab?.topicId) {
      setActiveTopicTurns(undefined);
      return () => {
        cancelled = true;
      };
    }
    void app.GetTopicSummary({
      scope: activeTab.scope === "global" ? "global" : "project",
      workspaceRoot: activeTab.scope === "global" ? "" : activeTab.workspaceRoot,
      topicId: activeTab.topicId,
    })
      .then((topic) => {
        if (!cancelled) setActiveTopicTurns(topic.turns);
      })
      .catch(() => {
        if (!cancelled) setActiveTopicTurns(undefined);
      });
    return () => {
      cancelled = true;
    };
  }, [activeTab?.scope, activeTab?.topicId, activeTab?.workspaceRoot, projectRevision]);
  const visibleUserTurns = visibleRuntimeState.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
  const currentTabTurns = Math.max(visibleRuntimeState.checkpoints.length, visibleUserTurns);
  const sessionTurns = currentTabTurns > 0 ? currentTabTurns : remoteSurfaceActive ? 0 : activeTopicTurns ?? 0;
  const startupSplashHold = !activeTabId && state.meta?.ready !== true && !state.meta?.startupErr;
  const activeComposerProfile = activeTabId ? composerProfilesByTab[activeTabId] : undefined;
  const backendActiveComposerProfile = useMemo(() => {
    if (state.meta) {
      return composerProfileFromMeta(
        state.meta,
        activeTab ? composerProfileMode(composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode)) : undefined,
        activeComposerProfile?.toolApprovalMode,
      );
    }
    return composerProfileFromTab(activeTab, activeComposerProfile?.toolApprovalMode);
  }, [activeComposerProfile?.toolApprovalMode, activeTab, state.meta]);
  const composerProfile = activeTabId
    ? activeComposerProfile ?? backendActiveComposerProfile
    : defaultComposerProfile;
  const goal = composerProfile.goal;
  const collaborationMode = displayedComposerProfileCollaborationMode(composerProfile);
  const toolApprovalMode = composerProfile.toolApprovalMode;
  const remoteComposerProfileReady = useRemoteComposerProfileSync({ activeTabId, remote: remoteSurfaceActive,
    remoteProfile: remoteSession.composerProfile, collaborationMode, toolApprovalMode, goal,
    qualityFloor: composerProfile.qualityFloor, pending: composerProfile.pending, setProfiles: setComposerProfilesByTab });
  const controllerReady =
    state.meta?.ready === true &&
    (!state.meta.runtime || state.meta.runtime.phase === "ready") &&
    !state.meta.startupErr &&
    !state.backendActivationPending &&
    !runtimeTransitioning;

  useEffect(() => {
    recordFrontendDiagnostic("app", "app.runtime-state", {
      ready: controllerReady,
      running: state.running,
      hydrating: state.hydrating,
      runtimeTransitioning,
      contentRevision: state.historyLayoutRevision,
    });
  }, [controllerReady, runtimeTransitioning, state.hydrating, state.historyLayoutRevision, state.running]);
  // Single footer decision surface. Composer stays mounted underneath and is
  // only visually/a11y-hidden so per-session draft caches survive.
  const decisionSurface = useMemo((): DecisionSurfaceKind | null => {
    if (state.approval) {
      return state.approval.tool === "exit_plan_mode" ? "plan_approval" : "tool_approval";
    }
    if (state.ask) return "ask";
    if (state.mcpInteraction) return "mcp_interaction";
    if (state.extensionForm) return "extension_form";
    if (workspaceConflict) return "workspace_conflict";
    if (pendingClose) return "close_active";
    if (clearContextPending) return "clear_context";
    return null;
  }, [clearContextPending, pendingClose, state.approval, state.ask, state.extensionForm, state.mcpInteraction, workspaceConflict]);
  const visibleDecisionSurface = decisionSurface;
  const composerSurfaceHidden = runtimeTransitioning || Boolean(decisionSurface);
  decisionSurfaceRef.current = decisionSurface;
  useEffect(() => {
    // Close composer menus/popovers when a decision takes over the footer.
    if (decisionSurface) {
      closeTransientOverlays();
      prevDecisionSurfaceRef.current = decisionSurface;
      return;
    }
    // Restore composer focus on the next frame only if the tab did not switch
    // and no new decision arrived (remote resolution / rapid consecutive prompts).
    const hadDecision = prevDecisionSurfaceRef.current != null;
    prevDecisionSurfaceRef.current = null;
    if (!hadDecision) return;
    const tabAtRelease = activeTabId;
    const frame = requestAnimationFrame(() => {
      if (decisionSurfaceRef.current != null) return;
      if (activeTabIdRef.current !== tabAtRelease) return;
      const input = document.getElementById("composer-input") as HTMLTextAreaElement | null;
      input?.focus({ preventScroll: true });
    });
    return () => cancelAnimationFrame(frame);
  }, [activeTabId, closeTransientOverlays, decisionSurface]);

  // Extension form surface (stage 8b2): submit delivers the structured values
  // to the owning sidecar; cancel reports values{"cancelled": true} over the
  // same channel. A failed cancel still dismisses — the sidecar that could not
  // be reached is gone either way.
  const [extensionFormBusy, setExtensionFormBusy] = useState(false);
  const submitExtensionForm = useCommittedCommand(async (values: Record<string, unknown>) => {
    const pending = state.extensionForm;
    if (!pending || !activeTabId || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, values);
      dismissExtensionForm();
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      setExtensionFormBusy(false);
    }
  });
  const cancelExtensionForm = useCommittedCommand(async () => {
    const pending = state.extensionForm;
    if (!pending || extensionFormBusy) return;
    setExtensionFormBusy(true);
    try {
      if (activeTabId) {
        await app.SubmitExtensionForm(activeTabId, pending.pluginId, pending.surfaceId, { cancelled: true }).catch(() => {});
      }
      dismissExtensionForm();
    } finally {
      setExtensionFormBusy(false);
    }
  });

  // Extension notifications queue in per-tab state (the reducer cannot reach
  // the toast context); drain the active tab's queue into toasts here.
  useEffect(() => {
    const pending = state.extensionNotifications;
    if (!pending || pending.length === 0) return;
    for (const notification of pending) {
      const level = notification.severity === "error" ? "error" : notification.severity === "warn" ? "warn" : "info";
      showToast(notification.body ? `${notification.title} — ${notification.body}` : notification.title, level);
    }
    drainExtensionNotifications();
  }, [state.extensionNotifications, showToast, drainExtensionNotifications]);
  const extensionStatusList = useMemo(() => Object.values(state.extensionStatuses ?? {}), [state.extensionStatuses]);
  const patchActiveComposerProfile = useCommittedCommand((patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
      if (!activeTabId) return;
      setComposerProfilesByTab((current) => patchComposerProfile(current, activeTabId, composerProfile, patch, pendingFields));
    });
  const patchComposerProfileForTab = useCommittedCommand((tabId: string, patch: Partial<Omit<ComposerProfile, "pending">>, pendingFields: ComposerProfileField[]) => {
      if (!tabId) return;
      setComposerProfilesByTab((current) => {
        const base = current[tabId] ?? composerProfileFromTab(tabMetas.find((tab) => tab.id === tabId));
        return patchComposerProfile(current, tabId, base, patch, pendingFields);
      });
    });
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => {
    const byId = new Map(tabMetas.map((tab) => [tab.id, tab]));
    const ordered = tabOrderIds.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
    const missing = tabMetas.filter((tab) => !tabOrderIds.includes(tab.id));
    return [...ordered, ...missing].map((tab) => {
      const profile = composerProfilesByTab[tab.id] ?? composerProfileFromTab(tab);
      return {
        ...tab,
        running: tab.id === visibleTabId ? tab.running || state.running : tab.running,
        mode: composerProfileMode(profile),
        collaborationMode: displayedComposerProfileCollaborationMode(profile),
        toolApprovalMode: profile.toolApprovalMode,
        goal: profile.goal,
        active: tab.id === visibleTabId,
      };
    });
  }, [composerProfilesByTab, state.running, tabMetas, tabOrderIds, visibleTabId]);

  useEffect(() => {
    const ids = tabMetas.map((tab) => tab.id);
    setTabOrderIds((current) => {
      const next = current.filter((id) => ids.includes(id));
      for (const id of ids) {
        if (!next.includes(id)) next.push(id);
      }
      return next.join("\u0000") === current.join("\u0000") ? current : next;
    });
  }, [tabMetas]);

  useEffect(() => {
    const ids = new Set(tabMetas.map((tab) => tab.id));
    for (const id of Object.keys(yoloRestoreToolApprovalModesRef.current)) {
      if (!ids.has(id)) delete yoloRestoreToolApprovalModesRef.current[id];
    }
    userPlanModeByTabRef.current = pruneUserPlanModeIntents(userPlanModeByTabRef.current, ids);
    setComposerProfilesByTab((current) => hydrateComposerProfilesFromTabs(current, tabMetas));
  }, [tabMetas]);


  useEffect(() => {
    if (!activeTabId || !state.meta) return;
    setComposerProfilesByTab((current) => hydrateComposerProfileFromMeta(current, activeTabId, state.meta!));
  }, [activeTabId, state.meta]);


  const applyQualityFloor = useCommittedCommand((floor: QualityFloor) => {
      if (!activeTabId) return;
      if (remoteSurfaceActive) {
        void remoteSession.setQualityFloor(floor).catch((error) => showToast(error instanceof Error ? error.message : String(error), "error"));
        return;
      }
      patchActiveComposerProfile({ qualityFloor: floor }, ["qualityFloor"]);
      void setControllerQualityFloor(floor);
    });
  const toggleYoloApprovalMode = useCommittedCommand(() => {
    if (!activeTabId) return;
    const next = toggleYoloToolApprovalMode(
      toolApprovalMode,
      yoloRestoreToolApprovalModesRef.current[activeTabId],
    );
    if (next.restore) {
      yoloRestoreToolApprovalModesRef.current[activeTabId] = next.restore;
    }
    applyToolApprovalMode(next.mode);
  });
  const patchActivatedGoalForTab = useCommittedCommand((tabId: string, nextGoal: string): void => {
      const trimmed = nextGoal.trim();
      patchComposerProfileForTab(tabId, {
        collaborationMode: trimmed ? "goal" : "normal",
        goalDraftMode: false,
        goal: trimmed,
      }, ["collaborationMode", "goal"]);
      userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, tabId, false);
    });
  const controllerProfiles = projectControllerProfiles(tabMetas, composerProfilesByTab, {
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, profile: composerProfile, remote: remoteSurfaceActive,
  });
  const { switchModel, switchModelFromUi, applyProfile: applyControllerProfile } = useControllerProfileCommands({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, profiles: controllerProfiles,
    ready: controllerReady, remote: remoteSurfaceActive, runtimeEpoch: state.meta?.runtime?.epoch, operations: sessionOperations,
    ports: { model: setModelForTab, profile: setControllerComposerProfileForTab },
    remoteModel: remoteSession.setModel, report: handleGoalActionError,
  });
  const clearSubmissionUndo = useCommittedCommand((tab: string) => setRewindStateForTab(tab, null));
  const { commitThenSend, submit: submitComposerTurn, applyGoalForTab, applyGoal, sendRevision } = useSessionSubmission({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, operations: sessionOperations,
    resources: projectSubmissionResources(controllerProfiles, tabMetas, composerProfilesByTab,
      { tabId: activeTabId ?? "", profile: composerProfile, ready: controllerReady },
      { starting: t("composer.workspaceStarting"), readOnly: t("composer.readOnlyChannel") }),
    missingSource: t("composer.workspaceStarting"),
    ports: createSubmissionPorts({ send: sendToTab, setGoal: setControllerGoalForTab, clearGoal: clearControllerGoalForTab,
      clearUndo: clearSubmissionUndo, patchGoal: patchActivatedGoalForTab, profile: applyControllerProfile }),
  });
  const notePlanModeForTab = useCommittedCommand((tabId: string, enabled: boolean) => {
    userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, tabId, enabled);
  });
  const patchPlanExitProfileForTab = useCommittedCommand((tabId: string, mode: CollaborationMode) => {
    patchComposerProfileForTab(tabId, {
      collaborationMode: mode,
      goalDraftMode: false,
      goal: "",
    }, ["collaborationMode", "goal"]);
  });
  const drainRemoteApprovalsForTab = useCommittedCommand((tabId: string, ids: string[]) => {
    if (activeTabId === tabId) remoteSession.drainApprovals(ids);
  });
  const rememberApprovalForTab = useCommittedCommand((tabId: string, previous: ToolApprovalMode, next: ToolApprovalMode) => {
    if (next !== "yolo") yoloRestoreToolApprovalModesRef.current[tabId] = restorableToolApprovalMode(next);
    else if (previous !== "yolo") yoloRestoreToolApprovalModesRef.current[tabId] = restorableToolApprovalMode(previous);
  });
  const { applyMode, applyCollaborationMode, applyToolApprovalMode } = useComposerModeActions({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    remote: remoteSurfaceActive, collaborationMode, toolApprovalMode, goal,
    operations: sessionOperations,
    ports: {
      setMode: setControllerModeForTab, setCollaboration: setControllerCollaborationModeForTab,
      setApproval: setToolApprovalModeForTab, clearGoal: clearControllerGoalForTab,
      setRemote: setRemoteComposerProfileForSessionAction, drainRemote: drainRemoteApprovalsForTab,
      patch: patchComposerProfileForTab, rememberPlan: notePlanModeForTab, rememberApproval: rememberApprovalForTab,
    },
    showError: (message) => showToast(message, "error"),
  });
  const rememberPlanRevisionForTab = usePendingPlanRevisions({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    resources: controllerProfiles.map(resource => resource.target), running: state.running,
    ready: controllerReady && !state.approval && !state.ask && !state.mcpInteraction,
    operations: sessionOperations, send: sendRevision, report: reportPendingRevisionFailure,
  });
  const { handleApprovalAnswer, handleRecoveryAnswer, handleRevisePlan, handleExitPlan,
    handleQuestionAnswer, handleQuestionDismiss, handleMCPAnswer } = useSessionPromptCommands({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    approval: state.approval ? { id: state.approval.id, tool: state.approval.tool } : undefined,
    questionId: state.ask?.id, remote: Boolean(activeTab?.remote), goal, toolApprovalMode,
    operations: sessionOperations,
    ports: {
      approveForTab, isPromptCurrentForTab, resolvePlanForTab: resolvePlanDecisionForTab,
      resolveRecoveryForTab, answerQuestionForTab, answerMCPForTab: answerMCPInteractionForTab,
      setCollaborationModeForTab: setControllerCollaborationModeForTab,
      clearGoalForTab: clearControllerGoalForTab, setRemoteComposerProfile: setRemoteComposerProfileForSessionAction,
      patchComposerProfile: patchPlanExitProfileForTab, notePlanMode: notePlanModeForTab,
      drainRemoteApprovals: drainRemoteApprovalsForTab, rememberRevision: rememberPlanRevisionForTab,
    },
    reportError: error => showToast(error instanceof Error ? error.message : String(error), "error"),
  });
  const remoteComposerSend = useRemoteComposerSend(activeTab?.remote, activeTabId, collaborationMode, goal,
    remoteSession, remoteSend, applyGoalForTab, useCommittedCommand(() => setClearContextPending(true)),
    { target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, operations: sessionOperations,
      navigateRemote: useCommittedCommand<RemoteNavigationCommand>((remote, options) => openRemoteProject(remote, options)) });
  const cancelRuntimeJob = useCommittedCommand(async (tabId: string, jobId: string): Promise<boolean> => {
    try {
      const cancelled = await app.CancelJobForTab(tabId, jobId);
      await refreshBackgroundRuntimes();
      return cancelled;
    } catch (err) {
      showToast(err instanceof Error ? err.message : String(err), "error");
      return false;
    }
  });
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCommittedCommand(() => {
    runGoalAction(() => applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan"));
  });

  // The live task list pinned above the composer comes from the most recent
  // successful top-level todo_write result; failed or still-running attempts do
  // not advance the canonical panel state. Incomplete lists are always shown so
  // a stale local dismissal cannot hide work that still blocks final readiness;
  // every new list starts collapsed while its header keeps showing live progress
  // and the current task. Live completion briefly shows 3/3 before retirement;
  // restored completed lists stay in transcript only. The dismissal key is
  // still based on stable todo content/state so history reloads do not
  // resurrect the same finished list. The status-agnostic batch key prevents
  // false new batches; dismissal remains session-scoped and sidecar-persisted.
  const todoEntry = useMemo(() => {
    for (let i = visibleRuntimeState.items.length - 1; i >= 0; i--) {
      const it = visibleRuntimeState.items[i];
      if (it.kind === "tool" && it.name === "todo_write" && !it.parentId && it.status === "done" && !it.error) {
        return { item: it, index: i };
      }
    }
    return null;
  }, [visibleRuntimeState.items]);
  const todoItem = todoEntry?.item ?? null;
  const metaTodos = remoteSurfaceActive ? undefined : state.meta?.canonicalTodos;
  const todos = useMemo(
    () => resolveTodoPanelTodos(metaTodos, todoItem ? parseTodos(todoItem.args) : undefined),
    [metaTodos, todoItem],
  );
  const [dismissedTodoKeys, setDismissedTodoKeys] = useState<Set<string>>(loadDismissedTodoKeys);
  const todoKey = useMemo(() => todoDismissalKey(todos), [todos]);
  const todoBatch = useMemo(() => todoBatchKey(todos), [todos]);
  const todoScope = useMemo(
    () => todoPanelScope({ activeTab, activeTabId, eventChannel: remoteSurfaceActive ? undefined : state.meta?.eventChannel }),
    [activeTab, activeTabId, remoteSurfaceActive, state.meta?.eventChannel],
  );
  const dismissedTodo = useMemo(
    () => dismissedTodoKeyForScope(todoScope, dismissedTodoKeys, todoKey),
    [dismissedTodoKeys, todoKey, todoScope],
  );
  const scopedTodoKey = useMemo(() => scopedTodoDismissalKey(todoScope, todoKey), [todoKey, todoScope]);
  const scopedTodoBatch = useMemo(() => scopedTodoBatchKey(todoScope, todoBatch), [todoBatch, todoScope]);
  const showTodos = shouldShowTodoPanel(todoKey, dismissedTodo, todos, { batchKey: todoBatch, batches: !remoteSurfaceActive && state.meta?.sessionPath === activeTab?.sessionPath ? state.meta?.dismissedTodoBatches : undefined });
  const dismissTodos = useCommittedCommand(() => {
    if (!scopedTodoKey) return;
    setDismissedTodoKeys((current) => {
      if (current.has(scopedTodoKey)) return current;
      const next = new Set(current);
      next.add(scopedTodoKey);
      saveDismissedTodoKeys(next);
      return next;
    });
    if (!remoteSurfaceActive && activeTabId && todoBatch) void app.DismissTodoBatchForTab(activeTabId, todoBatch).catch(() => undefined);
  });
  const handleTodoContinue = useCommittedCommand(() => {
    const targetTabId = todoContinueTarget(activeTabId, activeTabIdRef.current, {
      ready: remoteSurfaceActive ? remoteComposerReady : controllerReady,
      readOnly: Boolean(activeTab?.readOnly),
      running: visibleRuntimeState.running,
      pendingPrompt: visibleRuntimeState.pendingPrompt,
    });
    if (!targetTabId) return;
    const prompt = t("todo.continue");
    if (remoteSurfaceActive) {
      void remoteSend(prompt);
      return;
    }
    void sendToTab(targetTabId, prompt);
  });

  const sessionTitle = topicTitle(activeTab);
  const exportItems = remoteSurfaceActive ? remoteSession.transcript.items : state.items;
  const exportLive = remoteSurfaceActive
    ? remoteSession.transcript.live
    : liveStore.getSnapshot(activeTabId) ?? state.live;
  const sessionHasContent = exportItems.length > 0 || Boolean(exportLive?.text || exportLive?.reasoning);

  // Theme pack scene: home when the session is empty, task once content exists.
  useEffect(() => {
    applyThemeScene(sessionHasContent ? "task" : "home");
  }, [sessionHasContent]);
  const getSessionMarkdown = useCommittedCommand(async () => (await import("./lib/sessionExportData")).sessionItemsToMarkdown(sessionTitle, exportItems, exportLive));
  const getSessionJson = useCommittedCommand(async () => (await import("./lib/sessionExportData")).sessionItemsToJson(sessionTitle, exportItems, exportLive));

  useEffect(() => {
    if (!topicExportOpen) return;
    const onDown = (event: MouseEvent) => {
      const target = event.target as Element | null;
      if (!target?.closest(".topicbar__export")) setTopicExportOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [topicExportOpen]);

  const exportSession = useCommittedCommand(async (format: "markdown" | "json" | "pdf" | "image") => {
      const base = safeFilename(sessionTitle);
      setTopicExportOpen(false);
      try {
        if (format === "json") {
          const path = await app.PickExportFile(`${base}.json`, "application/json");
          if (path) {
            await app.SaveExportFile(path, await getSessionJson(), false);
            showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
          }
        } else if (format === "pdf") {
          const path = await app.PickExportFile(`${base}.pdf`, "application/pdf");
          if (!path) return;
          const { blobToBase64, renderSessionPdfBlob } = await import("./lib/sessionExport");
          const blob = await renderSessionPdfBlob(await getSessionMarkdown(), sessionTitle);
          await app.SaveExportFile(path, await blobToBase64(blob), true);
          showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
        } else if (format === "image") {
          const path = await app.PickExportFile(`${base}.png`, "image/png");
          if (!path) return;
          const { renderSessionImageBase64Payloads } = await import("./lib/sessionExport");
          const payloads = await renderSessionImageBase64Payloads(await getSessionMarkdown());
          await app.SaveExportImageFiles(path, payloads);
          showToast(
            payloads.length > 1
              ? t("topicBar.exportImageParts", { count: payloads.length })
              : t("topicBar.exportSuccess", { count: 1 }),
            "info",
          );
        } else {
          const path = await app.PickExportFile(`${base}.md`, "text/markdown");
          if (path) {
            await app.SaveExportFile(path, await getSessionMarkdown(), false);
            showToast(t("topicBar.exportSuccess", { count: 1 }), "info");
          }
        }
      } catch (err) {
        console.error("Failed to export session", err);
        showToast(
          t("topicBar.exportFailed", { error: err instanceof Error ? err.message : String(err) }),
          "error",
          { durationMs: 8000 },
        );
      }
    });


  useEffect(() => {
    setClearContextPending(false);
    setWorkspaceInsertTarget("composer");
  }, [activeTabId]);

  const cancelClearContext = useCommittedCommand(() => {
    setClearContextPending(false);
  });

  const confirmClearContext = useCommittedCommand(async () => {
    setClearContextPending(false);
    try {
      if (remoteSurfaceActive && activeTabId) {
        await app.ClearRemoteTabSession(activeTabId);
        await remoteSession.retryHydration();
      } else {
        await clearSession();
      }
      setDockRefreshKey((v) => v + 1);
      notice(t("clearContext.done"));
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      notice(msg || t("clearContext.failed"), "warn");
    }
  });

  useEffect(() => {
    activeTabIdRef.current = activeTabId;
  }, [activeTabId]);
  // handleSend reserves only commands that need a desktop-native UI action.
  const handleSend = useCommittedCommand(async (displayText: string, submitText = displayText, requestedTabId = activeTabId, structured?: StructuredInvocationSubmit) => {
      const sourceTabId = requestedTabId || activeTabId;
      if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
      const trimmed = displayText.trim();
      // "!<cmd>" runs a shell command directly, bypassing the model.
      if (trimmed.startsWith("!")) {
        const cmd = trimmed.slice(1).trim();
        if (!cmd) {
          notice("usage: !<command>  (e.g. !ls -la)");
          return;
        }
        await runShellForTab(sourceTabId, cmd);
        return;
      }
      const model = /^\/model\s+(\S+)$/.exec(trimmed);
      if (model) {
        await switchModel(model[1], sourceTabId);
        return;
      }
      if (trimmed === "/memory") {
        if (activeTabIdRef.current !== sourceTabId) return;
        closeTransientOverlays();
        setSettingsTarget("memory");
        return;
      }
      if (trimmed === "/clear") {
        if (activeTabIdRef.current !== sourceTabId) return;
        setClearContextPending(true);
        return;
      }
      if (trimmed === "/new") {
        if (activeTabIdRef.current !== sourceTabId) return;
        await newSession();
        return;
      }
      const decisionMock = typeof window !== "undefined" && !window.runtime
        ? decisionSurfaceMockFromInput(trimmed)
        : null;
      if (decisionMock === "workspace_conflict" || decisionMock === "mode_jobs" || decisionMock === "close_active" || decisionMock === "clear_context") {
        if (activeTabIdRef.current !== sourceTabId) return;
        closeTransientOverlays();
        setWorkspaceConflict(null);
        setPendingClose(null);
        setClearContextPending(false);
        const mockWork: ActiveWorkView = {
          running: true,
          pendingPrompt: false,
          cancellable: true,
          jobs: [
            { id: "mock-decision-build", kind: "bash", label: "pnpm build", status: "running", startedAt: Date.now() - 42_000 },
            { id: "mock-decision-test", kind: "bash", label: "go test ./...", status: "running", startedAt: Date.now() - 18_000 },
          ],
        };
        if (decisionMock === "workspace_conflict") {
          setWorkspaceConflict({
            state: "local",
            ownerTabId: "mock-workspace-writer",
            ownerTitle: t("mock.topicDevStandard"),
            ownerWork: mockWork,
            canReveal: true,
            canCreateWorktree: true,
          });
        } else if (decisionMock === "close_active") {
          setPendingClose({ tabId: sourceTabId, work: mockWork, stopping: false });
        } else {
          setClearContextPending(true);
        }
        return;
      }
      if (goalCommand(trimmed) || (collaborationMode === "goal" && !goal.trim())) {
        await submitComposerTurn(sourceTabId, displayText, submitText, structured);
        return;
      }
      const theme = /^\/theme(?:\s+(\S+))?$/.exec(trimmed);
      if (theme) {
        const arg = theme[1]?.toLowerCase();
        if (!arg) {
          const cur = getTheme();
          notice(t("settings.themeCurrent", { theme: cur, style: getThemeStyle(cur) }));
          return;
        }
        if (arg === "reset" || arg === "default" || arg === "clear") {
          try {
            await app.ResetThemePack();
            clearThemePack();
            notice(t("settings.themeReset"));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        if (isThemeMode(arg)) {
          const next = arg;
          const style = getThemeStyle(next);
          try {
            await app.SetDesktopAppearance(next, style);
            applyTheme(next, style);
            notice(t("settings.themeChanged", { theme: next, style }));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        if (isThemeStyle(arg)) {
          const cur = getTheme();
          try {
            await app.SetDesktopAppearance(cur, arg);
            applyTheme(cur, arg);
            notice(t("settings.themeChanged", { theme: cur, style: arg }));
          } catch (err) {
            showToast(err instanceof Error ? err.message : String(err), "error");
          }
          return;
        }
        notice(t("settings.themeUnknown", { name: arg }), "warn");
        return;
      }
      await submitComposerTurn(sourceTabId, displayText, submitText, structured);
    });

  const handleSteer = useCommittedCommand(async (text: string, requestedTabId = activeTabId) => {
    const sourceTabId = requestedTabId || activeTabId;
    if (!sourceTabId) throw new Error(t("composer.workspaceStarting"));
    if (tabMetas.some((tab) => tab.id === sourceTabId && tab.remote)) {
      await app.SteerRemoteTab(sourceTabId, text.trim());
      return;
    }
    await steerForTab(sourceTabId, text.trim());
  });

  const { setCollaborationModeFromUi, clearGoalFromUi } = useComposerGoalCommands({ applyCollaborationMode, applyGoal });
  const { pauseGoal: pauseGoalFromUi, resumeGoal: resumeGoalFromUi, setEffort: setEffortFromUi } = useRemoteComposerRuntimeActions({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, operations: sessionOperations,
    remote: remoteSurfaceActive, session: remoteSession, runGoalAction,
    pauseLocal: pauseControllerGoalForTab, resumeLocal: resumeControllerGoalForTab,
    setLocalEffort: setEffortForTab, showError: (message) => showToast(message, "error"),
  });

  const tabMetaRefreshCoordinatorRef = useRef<ReturnType<typeof createBoundedRefreshCoordinator<TabMeta[]>> | null>(null);
  if (!tabMetaRefreshCoordinatorRef.current) {
    tabMetaRefreshCoordinatorRef.current = createBoundedRefreshCoordinator<TabMeta[]>(TAB_META_MAX_IN_FLIGHT);
  }
  const refreshTabMetas = useCommittedCommand(async (
    apply?: () => boolean,
    options?: { afterMutation?: boolean },
  ): Promise<TabMeta[]> => {
    const result = await tabMetaRefreshCoordinatorRef.current!.run(
      async () => asArray(await app.ListTabs().catch(() => [] as TabMeta[])),
      options?.afterMutation ? { invalidate: true } : undefined,
    );
    const tabs = result.value;
    if (result.latest && (!apply || apply())) {
      setTabMetas((current) => sameTabMetaLists(current, tabs) ? current : tabs);
    }
    return tabs;
  });
  const seedActiveTabMeta = useCommittedCommand((tab: TabMeta): void => {
    setTabMetas((current) => seedActiveTabMetaList(current, tab));
    setTabOrderIds((current) => current.includes(tab.id) ? current : [...current, tab.id]);
  });
  const updateRemoteTabMeta = useCommittedCommand((tab: TabMeta): void => {
    setTabMetas((current) => current.map((existing) => existing.id === tab.id
      ? { ...existing, ...tab, active: existing.active }
      : existing));
  });

  const registerRemoteTabMeta = useCommittedCommand((tab: TabMeta) => {
    setTabMetas(current => current.some(existing => existing.id === tab.id) ? current : [...current, { ...tab, active: false }]);
  });
  useRemoteTabOpened(registerRemoteTabMeta, updateRemoteTabMeta);

  const handleRuntimeEvent = useCommittedCommand<RuntimeEventListener>((event) => {
    recordFrontendDiagnostic("runtime", "runtime.event", { action: event.kind, status: event.err ? "error" : "ok" });
    if (event.kind === "turn_done") {
      setDockRefreshKey((value) => value + 1);
      setProjectRevision((value) => value + 1);
      if (!event.err) playSuccessChime();
    }
    if (shouldPlayAttentionChimeForEvent(event, attentionChimeEvents.current)) playAttentionChime();
    if (shouldRefreshTabMetaForEvent(event.kind)) void refreshTabMetas(undefined, { afterMutation: true });
    if (event.kind !== "turn_done") return;
    const turnTabId = resolvePlanRestoreTabId(event.tabId, activeTabIdRef.current);
    void refreshTabMetas(undefined, { afterMutation: true }).then((tabs) => {
      if (!turnTabId) return;
      const tab = tabs.find((item) => item.id === turnTabId);
      const baseProfile = tab ? composerProfileFromTab(tab) : defaultComposerProfile;
      if (!shouldRestoreUserPlanModeForProfile(userPlanModeByTabRef.current, turnTabId, baseProfile)) {
        if (baseProfile.goal.trim()) {
          userPlanModeByTabRef.current = updateUserPlanModeIntent(userPlanModeByTabRef.current, turnTabId, false);
        }
        return;
      }
      setComposerProfilesByTab((current) => patchComposerProfile(
        current, turnTabId, current[turnTabId] ?? baseProfile,
        { collaborationMode: "plan", goalDraftMode: false, goal: "" },
        ["collaborationMode", "goal"],
      ));
      if (activeTabIdRef.current === turnTabId) void setControllerCollaborationMode("plan");
    });
  });

  const handleRuntimeReady = useCommittedCommand<RuntimeReadyListener>((readyTabId) => {
    recordFrontendDiagnostic("runtime", "runtime.ready", { ready: true, hasActiveTab: Boolean(readyTabId) });
    clearAttentionChimeKeys(attentionChimeEvents.current, readyTabId);
    void refreshTabMetas();
    if (!readyTabId || readyTabId === workspaceScopeActiveTabRef.current) {
      setWorkspaceControllerEpoch((value) => value + 1);
    }
  });

  const handleRuntimeRebuilt = useCommittedCommand<RuntimeRebuiltListener>((rebuiltTabId) => {
    recordFrontendDiagnostic("runtime", "runtime.rebuilt", { ready: true, hasActiveTab: Boolean(rebuiltTabId) });
    clearAttentionChimeKeys(attentionChimeEvents.current, rebuiltTabId);
    if (!rebuiltTabId || rebuiltTabId === workspaceScopeActiveTabRef.current) {
      setWorkspaceControllerEpoch((value) => value + 1);
    }
  });

  const blankSessionTarget = useCommittedCommand(() => {
    const activeWorkspaceRoot = activeTab?.scope === "project" ? activeTab.workspaceRoot || "" : "";
    const scope = activeWorkspaceRoot ? "project" : "global";
    return { scope, workspaceRoot: activeWorkspaceRoot };
  });

  useEffect(() => {
    let live = true;
    const ready = import("./lib/workspaceRefreshStore")
      .then(({ default: startWorkspaceFocusReconciliation }) => live ? startWorkspaceFocusReconciliation(activeTabId, workspaceScopeKey, refreshTabMetas) : undefined)
      .catch(() => undefined);
    const stopProjectTree = onProjectTreeChanged(() => {
      setProjectRevision((value) => value + 1);
      void refreshTabMetas(undefined, { afterMutation: true });
    });
    return () => {
      live = false;
      stopProjectTree();
      void ready.then((stop) => stop?.());
    };
  }, [activeTabId, refreshTabMetas, workspaceScopeKey]);

  const handleRemoteStatus = useCommittedCommand<RemoteStatusListener>((status) => {
    applyRemoteStatus(status);
    if (status.state === "stopped" && status.error) requestRemoteStatusPopover(status.hostId);
  });
  const handleRemoteForwards = useCommittedCommand<RemoteForwardsListener>((event) => setRemoteForwards(event.hostId, event.forwards));
  const handleRemoteServer = useCommittedCommand<RemoteServerListener>((server) => setRemoteServer(server));
  const handleInitialRemoteHosts = useCommittedCommand((hosts: Awaited<ReturnType<typeof app.RemoteHosts>>) => setRemoteHosts(hosts));
  const handleInitialRemoteStatuses = useCommittedCommand((statuses: Awaited<ReturnType<typeof app.RemoteConnectionStatuses>>) => hydrateRemoteStatuses(statuses));

  const refreshProviderSetupState = useCommittedCommand(() => probeProviderSetupState());

  const leaseBlockedTab = activeLeaseBlockedTab(tabMetas, activeTab?.id ?? activeTabId);
  const bannerCommands = useSessionBannerCommands({
    remote: Boolean(activeTab?.remote),
    reloadConfigWarnings,
  });
  const { reclaimSession, openTakeoverDialog, closeTakeoverDialog, openConfigFile, reloadConfigFile, showReleaseNotes } = bannerCommands;

  useEffect(() => {
    const el = footerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    let frame = 0;
    const update = () => {
      if (frame) window.cancelAnimationFrame(frame);
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const next = Math.round(el.getBoundingClientRect().height);
        if (Math.abs(footerHeightRef.current - next) < 2) return;
        footerHeightRef.current = next;
        setFooterHeight(next);
      });
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(el);
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      observer.disconnect();
    };
  }, []);

  const { openRightDockMode, closeWorkspacePanel, toggleWorkspacePanel, toggleWorkspaceMaximized,
    handleWorkspacePreviewModeChange, openRemoteDock } = useWorkspacePanelCommands({
    workspaceRoot: activeTab?.workspaceRoot ?? state.meta?.cwd ?? "",
    creation: desktopLayoutStyle === "creation", visible: surfaceWorkspacePanelRenderable,
    closeOverlays: closeTransientOverlays, clearLiveWidth: setLiveWorkspacePanelRenderWidth,
  });

  const verificationRevealSequenceRef = useRef(0);
  const [verificationRevealRequest, setVerificationRevealRequest] = useState<WorkspaceVerificationRevealRequest | null>(null);
  const openTurnVerification = useCommittedCommand((summary: WireCompletionSummary) => {
    openRightDockMode("changed");
    verificationRevealSequenceRef.current += 1;
    setVerificationRevealRequest({
      id: verificationRevealSequenceRef.current,
      summary,
      tabId: activeTabId ?? "",
      turnStartAt: state.turnStartAt,
      currentSummary: state.completionSummary,
    });
  });

  useEffect(() => { setVerificationRevealRequest(null); }, [activeTabId, state.completionSummary, state.turnStartAt]);

  const { toggleTerminalPanel, openTerminalForPath, closeTerminalPanel } = useTerminalPanelCommands({
    tabId: activeTabId, enabled: conversationView.localToolsEnabled, shortcutsEnabled: !managementActive,
  });

  const remoteWorkspaceLaunchGate = useRef(new RemoteWorkspaceLaunchGate());
  const launchRemoteWorkspace = useCommittedCommand(async (host: RemoteHostView, requestSeq: number) => {
    const lastWorkspace = await app.RemoteLastWorkspace(host.id).catch(() => "");
    const workspace = resolveRemoteWorkspace(lastWorkspace, host.defaultWorkspace);
    if (!remoteWorkspaceLaunchGate.current.isCurrent(host.id, requestSeq)) return;
    await publishNavigationIntent("remote-workspace");
    await app.OpenRemoteWorkspace(host.id, workspace);
  });

  const openRemoteWorkspaceFromStatus = useCommittedCommand((host: RemoteHostView) => {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void launchRemoteWorkspace(host, requestSeq).catch((err) => {
      showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
    });
  });

  const connectAndOpenRemoteWorkspace = useCommittedCommand(function connectRemoteWorkspace(host: RemoteHostView) {
    const requestSeq = remoteWorkspaceLaunchGate.current.begin(host.id);
    void (async () => {
      try {
        const status = useRemoteStore.getState().statuses[host.id]?.state;
        if (status !== "connected" && status !== "degraded") {
          // Clear any stale failure before the new generation starts; otherwise a
          // previous stopped+error snapshot could make the waiter reject before
          // the kernel's fresh connecting event reaches the frontend.
          useRemoteStore.getState().applyStatus({ hostId: host.id, state: "connecting" });
          await app.ConnectRemoteHost(host.id);
          await waitForRemoteConnection(host.id);
        }
      } catch (err) {
        if (err instanceof RemoteConnectionTimeoutError) {
          showToast(t("remote.error.timeout", { host: host.label }), "error", {
            actionLabel: t("remote.error.stopAndRetry"),
            durationMs: 10_000,
            onAction: () => {
              void app.DisconnectRemoteHost(host.id)
                .catch(() => undefined)
                .then(() => connectRemoteWorkspace(host));
            },
          });
          return;
        }
        // Connection failures are host-scoped. Keep the persistent error and its
        // recovery actions beside the Remote SSH status entry instead of
        // stretching a raw backend error across the native titlebar.
        requestRemoteStatusPopover(host.id);
        return;
      }

      try {
        await launchRemoteWorkspace(host, requestSeq);
      } catch (err) {
        showToast(err instanceof Error ? err.message : String(err), "error", { durationMs: 6000 });
      }
    })();
  });

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${sidebarRenderWidth}px`,
        "--chat-min-width": `${chatReservedWidth}px`,
        "--workspace-width": `${workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
        "--terminal-height": `${terminalSurfaceOpen ? liveTerminalHeight ?? terminalRenderHeight : 0}px`,
      }) as CSSProperties,
    [chatReservedWidth, liveTerminalHeight, sidebarRenderWidth, terminalRenderHeight, terminalSurfaceOpen, workspacePanelRenderWidth],
  );

  const addWorkspaceTextToComposer = useCommittedCommand((text: string) => {
    if (activeTabId && workspaceInsertTarget === "planRevision" && state.approval?.tool === "exit_plan_mode") {
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: state.approval.id,
        request: { id: Date.now(), text },
      });
      return;
    }
    if (activeTabId) {
      setComposerInsertRequestsByTab((current) => ({
        ...current,
        [activeTabId]: { id: Date.now(), text },
      }));
    }
  });

  const addTerminalOutputToComposer = useCommittedCommand(async (sessionId: string) => {
    if (!activeTabId) return;
    try {
      const output = await app.TerminalOutputForTab(activeTabId, sessionId);
      const formatted = formatTerminalOutputForComposer(output);
      if (!formatted) {
        showToast(t("terminal.noOutput"), "info");
        return;
      }
      addWorkspaceTextToComposer(formatted);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  });

  const addSelectedTextToComposer = useCommittedCommand((text: string, source?: SelectedTextInsertRequest["source"]) => {
    const selected = text.trim();
    if (!activeTabId || !selected) return;
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: selected, ...(source ? { source } : {}) },
    }));
  });

  const addTerminalSelectionToComposer = useCommittedCommand((text: string) => addSelectedTextToComposer(text, "terminal"));
  const addWorkspaceCodeToComposer = useCommittedCommand((path: string, code: string) => {
    if (!activeTabId || !code.trim()) return;
    if (workspaceInsertTarget === "planRevision" && state.approval?.tool === "exit_plan_mode") {
      // The plan-revision input is plain text and only consumes request.text,
      // so hand it the fenced rendering instead of a structured reference.
      setPlanRevisionInsertRequest({
        tabId: activeTabId,
        approvalId: state.approval.id,
        request: { id: Date.now(), text: formatSelectionReference(path, code) },
      });
      return;
    }
    selectedTextRequestIdRef.current += 1;
    setSelectedTextRequestsByTab((current) => ({
      ...current,
      [activeTabId]: { id: selectedTextRequestIdRef.current, text: code, path },
    }));
  });

  // Coalesce tab-bar switches through the same last-click-wins scheduler that
  // openTopic/blank/resume navigation uses, so rapidly clicking between two
  // running sessions can't run two switchTab() calls concurrently. Concurrent
  // switches race on the backend SetActiveTab/confirmBackendActiveTab ordering,
  // which lands events + hydration on the wrong session (#5352). switchTab's own
  // loadSessionDataForTab is already seq-guarded; this serializes the backend
  // activation around it.
  const tabSwitchSeqRef = useRef(0);
  const tabSwitchRunningRef = useRef(false);
  const tabSwitchPendingRef = useRef<PendingNavigationRequest<{ tabId: string; optimisticTab?: TabMeta; navigationIntentSeq: number }> | null>(null);
  const enterChatViewForTabNavigation = useCommittedCommand(() => {
    enterConversation();
  });
  const enqueueTabSwitch = useCommittedCommand((tabId: string, optimisticTab?: TabMeta): Promise<void> => {
      enterChatViewForTabNavigation();
      // Claim the shared navigation epoch at click time, before this request
      // can wait behind an older tab switch. That immediately invalidates any
      // in-flight blank/topic completion from a previous user intent.
      const navigationIntentSeq = noteNavigationIntent();
      beginNavigationSurface(navigationIntentSeq);
      return enqueueNavigationRequest(
        { seqRef: tabSwitchSeqRef, runningRef: tabSwitchRunningRef, pendingRef: tabSwitchPendingRef },
        { tabId, optimisticTab, navigationIntentSeq },
        async (request) => {
          try {
            if (!isNavigationIntentCurrent(request.navigationIntentSeq)) return;
            if (request.optimisticTab?.remote) await switchRemoteTab(request.optimisticTab, request.navigationIntentSeq);
            else await switchTab(request.tabId, request.optimisticTab, request.navigationIntentSeq);
            if (!isNavigationIntentCurrent(request.navigationIntentSeq)) return;
            await refreshTabMetas(
              () => isNavigationIntentCurrent(request.navigationIntentSeq),
              { afterMutation: true },
            );
          } finally {
            settleNavigationSurface(request.navigationIntentSeq);
          }
        },
      );
    });

  const revealBackgroundRuntime = useCommittedCommand(async (tabId: string): Promise<void> => {
    enterChatViewForTabNavigation();
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealBackgroundRuntime(tabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-background",
        isIntentCurrent: isNavigationIntentCurrent,
        reassert: reassertVisibleTabAfterStaleNavigation,
      })) return;
      await switchTab(meta.id, meta, navigationIntentSeq);
      if (!isNavigationIntentCurrent(navigationIntentSeq)) return;
      await refreshTabMetas(
        () => isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  });

  const handleTabChange = useCommittedCommand((id: string) => {
    closeTransientOverlays();
    const selected = tabMetas.find((tab) => tab.id === id);
    setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === id })));
    void enqueueTabSwitch(id, selected);
    setTabRevealSignal((signal) => signal + 1);
  });

  const finishTabClose = useCommittedCommand(async (
    id: string,
    policy: "keep_running" | "stop_and_close",
  ): Promise<boolean> => {
    closeTransientOverlays();
    const closed = await closeTab(id, policy);
    if (!closed) {
      showToast(t("runtime.closeFailed"), "error");
      return false;
    }
    setComposerProfilesByTab((current) => {
      if (!(id in current)) return current;
      const next = { ...current };
      delete next[id];
      return next;
    });
    setTabMetas((current) => {
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
    await refreshTabMetas(undefined, { afterMutation: true });
    await refreshBackgroundRuntimes();
    setTabRevealSignal((signal) => signal + 1);
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

  const resolvePendingClose = useCommittedCommand(async (policy: "keep_running" | "stop_and_close") => {
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
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      const meta = await app.RevealWorkspaceWriterForTab(activeTabId);
      if (!await guardBackendNavigationResult({
        intent: navigationIntentSeq,
        targetTabId: meta.id,
        kind: "tab.reveal-workspace-writer",
        isIntentCurrent: isNavigationIntentCurrent,
        reassert: reassertVisibleTabAfterStaleNavigation,
      })) return;
      setWorkspaceConflict(null);
      await switchTab(meta.id, meta, navigationIntentSeq);
      if (!isNavigationIntentCurrent(navigationIntentSeq)) return;
      await refreshTabMetas(
        () => isNavigationIntentCurrent(navigationIntentSeq),
        { afterMutation: true },
      );
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  });

  const continueInDeliveryWorktree = useCommittedCommand(async () => {
    const root = state.meta?.workspaceRoot || state.meta?.workspacePath || state.meta?.cwd;
    if (!root) return;
    void handleCancelActive();
    setWorkspaceConflict(null);
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    try {
      await createIsolatedWorktree(root, navigationIntentSeq);
      await refreshTabMetas(undefined, { afterMutation: true });
    } catch (err) {
      if (isNavigationIntentCurrent(navigationIntentSeq)) showToast(err instanceof Error ? err.message : String(err), "error");
    } finally {
      settleNavigationSurface(navigationIntentSeq);
    }
  });

  const handleTabsClose = useCommittedCommand(async (ids: string[], nextActiveTabId?: string) => {
    closeTransientOverlays();
    const currentIds = tabMetas.map((tab) => tab.id);
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
      const selected = tabMetas.find((tab) => tab.id === nextActiveTabId);
      setTabMetas((current) => current.map((tab) => ({ ...tab, active: tab.id === nextActiveTabId })));
      void enqueueTabSwitch(nextActiveTabId, selected);
    }
    await refreshTabMetas(undefined, { afterMutation: true });
    setTabRevealSignal((signal) => signal + 1);
  });

  const handleTabsReorder = useCommittedCommand(async (ids: string[]) => {
    setTabOrderIds(ids);
    setTabMetas((current) => {
      const byId = new Map(current.map((tab) => [tab.id, tab]));
      const ordered = ids.map((id) => byId.get(id)).filter((tab): tab is TabMeta => Boolean(tab));
      return ordered.length === current.length ? ordered : current;
    });
    await reorderTabs(ids);
    await refreshTabMetas(undefined, { afterMutation: true });
    setTabRevealSignal((signal) => signal + 1);
  });

  const [rewindSignal, setRewindSignal] = useState(0);

  // ── Immediate rewind ──────────────────────────────────────────────────
  // On confirm, call Go prepare+commit immediately. Only after success does
  // the UI truncate the transcript, refresh files, and fill the composer.
  // Real backend undo uses UndoRewindForTab when a transaction id is available.
  const [rewindCommittingByTab, setRewindCommittingByTab] = useState<Record<string, boolean>>({});
  const rewindState = activeTabId ? rewindStatesByTab[activeTabId] ?? null : null;
  const rewindCommitting = Boolean(activeTabId && rewindCommittingByTab[activeTabId]);


  const setRewindCommittingForTab = useCommittedCommand((tabId: string, committing: boolean) => {
    setRewindCommittingByTab((current) => {
      const next = { ...current };
      if (committing) next[tabId] = true;
      else delete next[tabId];
      return next;
    });
  });

  const handleSessionRevertCommitted = useCommittedCommand((sourceTabId: string, outcome: RewindResultView) => {
    if (!sourceTabId || !outcome.ok) return;
    setRewindStateForTab(sourceTabId, {
      turnDiff: 0,
      transactionId: outcome.transactionId,
      undoAvailable: outcome.undoAvailable,
      filesRestored: outcome.written ?? [],
      filesRemoved: outcome.deleted ?? [],
    });
    setDockRefreshKey((value) => value + 1);
    setProjectRevision((value) => value + 1);
  });

  const hydratePlaceholderActive = Boolean(
    state.hydrating &&
    state.items.length === 0 &&
    state.hydratePlaceholderItems?.length,
  );
  const transcriptHydrating = state.hydrating && !state.hydrateHistoryLoaded;
  // Creation hero only after history hydration settles on a truly empty session.
  // Avoid flash while switching tabs: items may be empty while placeholders show.
  // Exclude IM/Bot detail: hero CSS collapses .main, which also hosts that panel.
  // (desktopLayoutStyle is available here; sidebarCreation is declared later.)
  const creationEmptyHero =
    desktopLayoutStyle === "creation" &&
    !runtimeTransitioning &&
    !sidebarImDetailConnection &&
    !sessionHasContent &&
    !transcriptHydrating &&
    !hydratePlaceholderActive;
  const transcriptItems = hydratePlaceholderActive ? state.hydratePlaceholderItems! : state.items;
  const handleLoadOlderHistory = useCommittedCommand((targetTurn?: number, trigger: HistoryLoadTrigger = "retry") => {
    return activeTabId ? loadOlderHistory(activeTabId, targetTurn, trigger) : Promise.resolve(false);
  });

  // Display items: backend history is authoritative after immediate commit.
  // rewindState only drives the undo banner, not optimistic truncation.
  const displayItems = transcriptItems;
  const committedSurfaceItems = remoteSurfaceActive ? remoteSession.transcript.items : displayItems;
  const committedGeometryKey = remoteSurfaceActive ? `tab:${activeTabId ?? "preview"}` : transcriptGeometrySessionKey;
  // Only committed presentation can become a future retained source surface.
  // A suspended or abandoned render must never become navigation authority.
  useLayoutEffect(() => {
    if (runtimeTransitioning) return;
    commitRenderedTranscriptSurface({
      tabId: activeTabId,
      items: committedSurfaceItems,
      geometrySessionKey: committedGeometryKey,
    });
  }, [activeTabId, commitRenderedTranscriptSurface, committedSurfaceItems, runtimeTransitioning, committedGeometryKey]);
  const visibleTranscriptSurface = runtimeTransitioning && !navigationTargetDataReady && preservedTranscriptSurface
    ? preservedTranscriptSurface
    : null;
  const visibleTranscriptItems = visibleTranscriptSurface?.items ?? displayItems;
  const visibleTranscriptTabId = visibleTranscriptSurface?.tabId ?? activeTabId;
  const visibleTranscriptGeometryKey = visibleTranscriptSurface?.geometrySessionKey ?? transcriptGeometrySessionKey;
  const handleSurfacePaintReady = useCommittedCommand((token: string, outcome: "ready" | "degraded") => {
    const receipt = commitNavigationSurfacePaint(token, outcome);
    if (singleSurfaceLayout && receipt) commitSingleSurfaceNavigation(receipt.targetTabId);
  });
  const latestGuidanceConsumed = useMemo(() => {
    for (let i = state.items.length - 1; i >= 0; i--) {
      const item = state.items[i];
      if (item.kind === "notice" && item.text.startsWith("↪ ")) {
        return { key: item.id, itemId: item.inboxItemId, text: item.text.slice(2) };
      }
    }
    return null;
  }, [state.items]);

  const handleTranscriptPrompt = useCommittedCommand((text: string) => {
    if (!activeTabId || !controllerReady) return;
    void commitThenSend(activeTabId, text).catch((err) => {
      console.warn("Failed to submit transcript prompt", err);
    });
  });

  const sendDeliveryRecovery = useCommittedCommand((tabId: string) => (
    recoverDeliveryToTab(tabId, t("notice.deliveryIncompleteContinuePrompt"))
  ));
  const handleDeliveryContinue = useCommittedCommand(() => {
    const ownership = sessionSurfaceFence.capture();
    return lazyContinueDelivery({
      tabId: ownership?.tabId,
      ready: controllerReady,
      goal: state.meta?.goal,
      uiOwnership: ownership,
      ownsUI: sessionSurfaceFence.ownsUnknown,
      resumeGoal: resumeControllerGoalForTab,
      send: sendDeliveryRecovery,
    });
  });
  const handleCancelActive = useCommittedCommand((queuedItemIDs: string[] = []) => {
    const sourceTabId = activeTabId;
    return sourceTabId ? cancelForTab(sourceTabId, queuedItemIDs) : cancel(queuedItemIDs);
  });
  const cancelWorkspaceConflict = useCommittedCommand(() => {
    void handleCancelActive();
    setWorkspaceConflict(null);
  });

  const handleMessageAction = useCommittedCommand((turn: number, scope: string) => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || activeTab?.readOnly) return;
    if (hydratePlaceholderActive) return;
    if (scope === "fork") {
      // Fork still goes through the controller (not optimistic).
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        void refreshTabMetas(undefined, { afterMutation: true });
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Code-only rewind only affects files — no message truncation,
    // no optimistic UI needed.  Execute immediately.
    if (scope === "code") {
      setRewindCommittingForTab(sourceTabId, true);
      void rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
        setRewindCommittingForTab(sourceTabId, false);
        if (!outcome.ok) return;
        setRewindStateForTab(sourceTabId, {
          turnDiff: 0,
          transactionId: outcome.transactionId,
          undoAvailable: outcome.undoAvailable,
          filesRestored: outcome.written ?? [],
          filesRemoved: outcome.deleted ?? [],
        });
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    // Summarize only compresses the conversation log — no files touched,
    // no optimistic UI needed. Execute immediately like code-only rewind.
    if (scope === "summ-from" || scope === "summ-upto") {
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      });
      return;
    }

    const items = state.items;
    const hasCheckpointTurns = items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let boundaryIdx = -1;
    let userCount = 0;
    let targetUserCount = -1;
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === "user") {
        const item = items[i] as Extract<Item, { kind: "user" }>;
        const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
        if (matches) {
          boundaryIdx = i;
          targetUserCount = userCount;
          break;
        }
        userCount++;
      }
    }
    if (boundaryIdx < 0) {
      rewindForTab(sourceTabId, turn, scope).then((ok) => {
        if (!ok) return;
        if (scope === "both") {
          setDockRefreshKey((v) => v + 1);
          setProjectRevision((v) => v + 1);
        }
      });
      return;
    }

    const prevUserCount = items.filter((it) => it.kind === "user").length;
    const turnDiff = prevUserCount - targetUserCount;
    const userItem = items[boundaryIdx]?.kind === "user" ? items[boundaryIdx] as Extract<Item, { kind: "user" }> : undefined;
    const prompt = userItem?.text ?? "";

    // Immediate backend commit — only update UI after success.
    setRewindCommittingForTab(sourceTabId, true);
    void rewindForTabDetailed(sourceTabId, turn, scope).then((outcome) => {
      setRewindCommittingForTab(sourceTabId, false);
      if (!outcome.ok) {
        // Keep conversation/files as-is; notices already carry the reason.
        return;
      }
      const targetTabId = outcome.tabId || sourceTabId;
      setRewindStateForTab(targetTabId, {
        turnDiff: outcome.tabId ? 0 : turnDiff,
        transactionId: outcome.transactionId,
        undoAvailable: outcome.undoAvailable,
        undoTabId: sourceTabId,
        filesRestored: outcome.written ?? [],
        filesRemoved: outcome.deleted ?? [],
      });
      const insertId = Date.now();
      setComposerInsertRequestsByTab((current) => ({
        ...current,
        [targetTabId]: { id: insertId, text: prompt, mode: "replace" },
      }));
      setRewindSignal((v) => v + 1);
      if (scope === "both" || scope === "code") {
        setDockRefreshKey((v) => v + 1);
        setProjectRevision((v) => v + 1);
      }
    });
  });

  const handleEditPrompt = useCommittedCommand(async (turn: number, displayText: string, submitText?: string): Promise<boolean> => {
    const sourceTabId = activeTabId;
    if (!sourceTabId || activeTab?.readOnly || !controllerReady || hydratePlaceholderActive || rewindStatesByTab[sourceTabId] || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending) return false;
    const next = displayText.trim();
    if (!next) return false;
    const submit = (submitText ?? displayText).trim();
    const hasCheckpointTurns = state.items.some((it) => it.kind === "user" && it.checkpointTurn != null);
    let original = "";
    let userCount = 0;
    for (const item of state.items) {
      if (item.kind !== "user") continue;
      const matches = hasCheckpointTurns ? item.checkpointTurn === turn : userCount === turn;
      if (matches) {
        original = (item.submitText ?? item.text).trim();
        break;
      }
      userCount++;
    }
    const outcome = await rewindForTabDetailed(sourceTabId, turn, "conversation");
    if (!outcome.ok) return false;
    setRewindSignal((v) => v + 1);
    const targetTabId = outcome.tabId || sourceTabId;
    try {
      await sendToTab(targetTabId, next, submit, original);
      return true;
    } catch {
      return false;
    }
  });

  const openTrash = useCommittedCommand(async () => {
    closeTransientOverlays();
    setHistView(null);
    openPage({ kind: "trash" });
  });
  const closeHistory = useCommittedCommand(() => {
    closeTransientOverlays();
    setHistView(null);
  });
  const refreshHistoryView = useCommittedCommand(async () => {
    const sessions = await listSessions().catch(() => null);
    if (!sessions) return;
    setHistView((cur) =>
      cur === null || cur.kind !== "history"
        ? cur
        : cur.source === "scope"
          ? { ...cur, sessions: sessionsForScope(sessions, cur.filter) }
          : { ...cur, sessions },
    );
  });

  const { openAutomationTopic, topicAccepted } = useAutomationNavigation({ noteIntent: noteNavigationIntent,
    enqueue: useCommittedCommand((intent, seq) => enqueueNavigationWithIntent(intent, seq)) });
  const { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject } = useDesktopNavigation({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, singleSurface: singleSurfaceLayout,
    ports: { isNavigationIntentCurrent, activateTopic, openTopicSession, openGlobalTab, openProjectTab,
      ensureBlankSurface, ensureBlankTab, createIsolatedWorktree, openChannelSession, resumeSession,
      registeredNavigationIntent, switchRemoteTab, openRemoteProject: app.OpenRemoteProjectTab,
      listTabs: app.ListTabs, applyTabs: setTabMetas, seedTab: seedActiveTabMeta, listSessions, topicAccepted },
    setTabRevealSignal, setTranscriptRevealSignal, setProjectRevision, setHistory: setHistView, t, showToast,
    noteIntent: noteNavigationIntent, beginSurface: beginNavigationSurface, settleSurface: settleNavigationSurface,
    showChat: enterConversation,
  });

  const openBlankSession = useCommittedCommand((scope: string, workspaceRoot: string): Promise<void> =>
    enqueueNavigation({ kind: "blank", scope, workspaceRoot: scope === "project" ? workspaceRoot : "" }));

  const handleNewTab = useCommittedCommand(async () => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    if (activeTab?.remote) {
      const outcome = await openRemoteProject(activeTab.remote, { newSession: true });
      if (outcome.status === "failed") showToast(outcome.error instanceof Error ? outcome.error.message : String(outcome.error), "error");
      return;
    }
    const target = blankSessionTarget();
    await openBlankSession(target.scope, target.workspaceRoot);
  });

  const handleOpenTopic = useCommittedCommand((scope: string, workspaceRoot: string, topicId: string, sessionPath?: string): Promise<void> => {
    closeTransientOverlays();
    setSidebarImDetailConnectionId("");
    return enqueueNavigation({ kind: "topic", scope, workspaceRoot, topicId, sessionPath });
  });

  const openSidebarImConnectionSession = useCommittedCommand((connection: SidebarImConnection): Promise<void> => {
    setSidebarImDetailConnectionId("");
    return enqueueNavigation({ kind: "sidebar-im", connection });
  });

  const onResumeSession = useCommittedCommand((session: SessionMeta): Promise<void> => {
    if (state.running && !singleSurfaceLayout) return Promise.resolve();
    return enqueueNavigation({ kind: "resume-session", session });
  });

  const onRecoveryCreated = useCommittedCommand(() => {
    setProjectRevision((value) => value + 1);
    void refreshTabMetas(undefined, { afterMutation: true });
  });
  const onRecoveryLineageChanged = useCommittedCommand(() => {
    setProjectRevision((value) => value + 1);
    void refreshHistoryView();
  });

  const openTaskMonitorSession = useCommittedCommand(async (tabID: string, taskID: string): Promise<boolean> => {
    if (state.running && !singleSurfaceLayout) {
      throw new Error(t("history.failedOpenSession"));
    }
    // Claim the navigation epoch before the first Wails await. If the user
    // switches tabs while the task/session lookup is pending, its completion is
    // stale and must not enqueue a newer navigation request.
    const navigationIntentSeq = noteNavigationIntent();
    beginNavigationSurface(navigationIntentSeq);
    let session: SessionMeta | null;
    try {
      session = await resolveTaskMonitorSession({
        tabID,
        taskID,
        intentSeq: navigationIntentSeq,
        isIntentCurrent: isNavigationIntentCurrent,
        openTaskSessionForTab: (sourceTabID, sourceTaskID) => app.OpenTaskSessionForTab(sourceTabID, sourceTaskID),
        listSessionsForTab: async (sourceTabID) => asArray(await app.ListSessionsForTab(sourceTabID)),
        sessionIDFromPath: taskSessionIDFromPath,
      });
    } catch (error) {
      settleNavigationSurface(navigationIntentSeq);
      throw error;
    }
    if (!session) {
      settleNavigationSurface(navigationIntentSeq);
      return false;
    }
    await enqueueNavigationWithIntent({ kind: "resume-session", session }, navigationIntentSeq);
    return isNavigationIntentCurrent(navigationIntentSeq);
  });

  // Command palette: ⌘K / Ctrl+K opens a fuzzy navigator over commands and
  // recent sessions. Sessions are snapshotted on open so the list is stable
  // while the palette is up; extension actions follow the same snapshot rule.
  const openPalette = useCommittedCommand(async () => {
    closeTransientOverlays();
    setPaletteOpen(true);
    setPaletteSessions(await listSessions().catch(() => []));
    setPaletteExtensionActions(await app.ExtensionActions(activeTabIdRef.current ?? "").catch(() => []));
  });
  useGlobalShortcut("commandPalette.open", () => {
    setPaletteOpen((current) => {
      if (!current) void openPalette();
      return !current; // ← fix: toggle the state so the palette actually opens/closes
    });
  }, [openPalette]);
  useGlobalShortcut("app.newSession", () => void handleNewTab(), [handleNewTab]);
  useGlobalShortcut("settings.open", () => {
    closeTransientOverlays();
    setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget);
  }, [closeTransientOverlays]);
  useGlobalShortcut("tab.close", () => {
    if (managementActive) returnToWorkspace();
    else if (activeTabId) void handleTabClose(activeTabId);
  }, [activeTabId, handleTabClose, managementActive, returnToWorkspace], managementActive || Boolean(activeTabId));
  useGlobalShortcut("shortcuts.show", () => setShortcutsOpen(true));
  useGlobalShortcut("sidebar.toggle", toggleSidebar, [toggleSidebar], !managementActive);

  // --- Topic shortcut navigation (Cmd/Ctrl+1-9) ---
  const visibleTopicsRef = useRef<TopicShortcutEntry[]>([]);
  const handleVisibleTopicsChange = useCommittedCommand((topics: TopicShortcutEntry[]) => {
    visibleTopicsRef.current = topics;
  });
  const handleNavigateTopic = useCommittedCommand((entry: TopicShortcutEntry) => {
    void handleOpenTopic(entry.scope, entry.workspaceRoot, entry.topicId, entry.sessionPath);
  });
  const { showBadges: showTopicBadges } = useTopicShortcuts(!sidebarCollapsed && !managementActive, desktopPlatform);

  // Register Cmd/Ctrl+1-9 shortcuts for topic navigation
  useEffect(() => {
    if (sidebarCollapsed || managementActive) return;
    const onKeydown = (event: globalThis.KeyboardEvent) => {
      const idx = topicShortcutIndexFromEvent(event, desktopPlatform);
      if (idx === null) return;
      event.preventDefault();
      const topics = visibleTopicsRef.current;
      if (idx < topics.length) {
        handleNavigateTopic(topics[idx]);
      }
    };
    document.addEventListener("keydown", onKeydown);
    return () => document.removeEventListener("keydown", onKeydown);
  }, [sidebarCollapsed, managementActive, desktopPlatform, handleNavigateTopic]);

  const paletteItems = useMemo<PaletteItem[]>(() => {
    const cmds: PaletteItem[] = [
      { id: "cmd-new", group: t("palette.group.commands"), title: t("palette.cmd.newSession"), icon: <SquarePen size={15} />, compact: true, keywords: ["new", "新建"], run: () => void handleNewTab() },
      { id: "cmd-automation", group: t("palette.group.commands"), title: t("sidebar.automation"), icon: <AlarmClock size={15} />, compact: true, keywords: ["automation", "自动化"], run: () => openPage({ kind: "automation" }) },
      { id: "cmd-trash", group: t("palette.group.commands"), title: t("palette.cmd.trash"), icon: <Trash2 size={15} />, compact: true, keywords: ["trash", "回收站"], run: () => void openTrash() },
      { id: "cmd-settings", group: t("palette.group.commands"), title: t("palette.cmd.settings"), icon: <SettingsIcon size={15} />, compact: true, keywords: ["settings", "设置"], run: () => setSettingsTarget(useAppNavigationStore.getState().lastSettingsTarget) },
      { id: "cmd-appearance", group: t("palette.group.commands"), title: t("palette.cmd.appearance"), icon: <Palette size={15} />, compact: true, keywords: ["theme", "appearance", "外观", "主题"], run: () => setSettingsTarget("appearance") },
      {
        id: "cmd-theme-reset",
        group: t("palette.group.commands"),
        title: t("settings.themeLibrary.reset"),
        icon: <Palette size={15} />,
        compact: true,
        keywords: ["theme", "reset", "default", "恢复默认", "主题"],
        run: () => {
          void app.ResetThemePack()
            .then(() => {
              clearThemePack();
              notice(t("settings.themeReset"));
            })
            .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
      { id: "cmd-memory", group: t("palette.group.commands"), title: t("palette.cmd.memory"), icon: <Brain size={15} />, compact: true, keywords: ["memory", "记忆"], run: () => setSettingsTarget("memory") },
      { id: "cmd-models", group: t("palette.group.commands"), title: t("palette.cmd.models"), icon: <Cpu size={15} />, compact: true, keywords: ["model", "模型"], run: () => setSettingsTarget("models") },
      {
        id: "cmd-usage-stats",
        group: t("palette.group.commands"),
        title: t("palette.cmd.usageStats"),
        icon: <BarChart3 size={15} />,
        compact: true,
        keywords: ["usage", "stats", "statistics", "用量", "统计"],
        run: () => {
          setSettingsFocus((current) => ({
            target: "model-stats",
            requestId: (current?.requestId ?? 0) + 1,
          }));
          setSettingsTarget("models");
        },
      },
      { id: "cmd-task-center", group: t("palette.group.commands"), title: t("palette.cmd.taskCenter"), icon: <Activity size={15} />, compact: true, keywords: ["task", "tasks", "center", "任务", "任务中心"], run: () => setTasksOpen("all") },
      { id: "cmd-terminal", group: t("palette.group.commands"), title: t("rightDock.terminal"), icon: <TerminalSquare size={15} />, compact: true, keywords: ["terminal", "shell", "终端"], run: () => toggleTerminalPanel() },
      {
        id: "cmd-reload-runtime",
        group: t("palette.group.commands"),
        title: t("palette.cmd.reloadRuntime"),
        icon: <RotateCw size={15} />,
        compact: true,
        keywords: ["reload", "runtime", "重载", "运行时"],
        run: () => {
          const tabID = activeTab?.id;
          if (!tabID) return;
          // Success/queued feedback arrives as a tab notice; only hard failures need a toast.
          void app.ReloadRuntime(tabID).catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
        },
      },
    ];
    const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
    const dayLabel = (ms: number) => {
      const days = Math.round((startOfDay(new Date()) - startOfDay(new Date(ms))) / 86_400_000);
      if (days <= 0) return t("history.today");
      if (days === 1) return t("history.yesterday");
      return new Date(ms).toLocaleDateString();
    };
    const sessionItems: PaletteItem[] = paletteSessions.slice(0, 12).map((s) => ({
      id: `sess-${s.path}`,
      group: t("palette.group.sessions"),
      title: paletteSessionDisplayTitle(s, t("history.emptySession")),
      hint: paletteSessionHint(s),
      keywords: paletteSessionKeywords(s),
      meta: dayLabel(sessionActivityTime(s)),
      badge: t(s.turns === 1 ? "history.turnOne" : "history.turnOther", { n: s.turns }),
      run: () => void onResumeSession(s),
    }));
    const remoteItems: PaletteItem[] = remoteHosts.map((host) => {
      const status = remoteStatuses[host.id];
      const connected = status?.state === "connected" || status?.state === "degraded";
      const target = `${host.user ? `${host.user}@` : ""}${host.host}${host.port && host.port !== 22 ? `:${host.port}` : ""}`;
      return {
        id: `remote-${host.id}`,
        group: t("palette.group.remote"),
        title: connected
          ? t("palette.remote.open", { host: host.label })
          : t("palette.remote.connect", { host: host.label }),
        hint: host.defaultWorkspace || target,
        icon: <Server size={15} />,
        keywords: ["ssh", "remote", "远程", "连接", host.label, host.host],
        run: () => {
          if (connected) openRemoteWorkspaceFromStatus(host);
          else connectAndOpenRemoteWorkspace(host);
        },
      };
    });
    const extensionItems: PaletteItem[] = paletteExtensionActions.map((action) => ({
      id: `ext-${action.slash}`,
      group: t("palette.group.extensions"),
      title: action.description || action.slash,
      hint: action.slash,
      icon: <Puzzle size={15} />,
      keywords: ["extension", "扩展", action.plugin, action.action, action.slash],
      run: () => {
        const tabID = activeTab?.id;
        if (!tabID) return;
        // The extension's result message is user-facing feedback; only hard
        // failures need an error toast.
        void app.InvokeExtensionAction(tabID, action.slash, {})
          .then((message) => {
            if (message) showToast(message, "info");
          })
          .catch((err) => showToast(err instanceof Error ? err.message : String(err), "error"));
      },
    }));
    return [...(remoteSurfaceActive ? cmds.filter((item) => item.id !== "cmd-terminal" && item.id !== "cmd-reload-runtime") : cmds), ...extensionItems, ...remoteItems, ...sessionItems];
  }, [t, paletteSessions, paletteExtensionActions, remoteHosts, remoteStatuses, activeTab?.id, handleNewTab, openTrash, onResumeSession, openRemoteWorkspaceFromStatus, connectAndOpenRemoteWorkspace, openRightDockMode, remoteSurfaceActive, showToast]);
  // Delete / rename act on disk, then re-fetch so the panel reflects the change.
  const onDeleteSession = useCommittedCommand(async (path: string) => {
      if (state.running) return;
      try {
        await deleteSession(path);
      } catch {
        await refreshHistoryView();
        return;
      }
      // Local state removal: filter the deleted session out of the current
      // history view instead of re-fetching the full list from the backend.
      setHistView((cur) =>
        cur === null || cur.kind !== "history"
          ? cur
          : { ...cur, sessions: cur.sessions.filter((s) => s.path !== path) },
      );
    });
  const onRenameHistorySession = useCommittedCommand(async (session: SessionMeta, title: string) => {
      if (state.running) return;
      if (session.topicId) await app.RenameTopic(session.topicId, title);
      else await renameSession(session.path, title);
      const sessions = await listSessions();
      setHistView((cur) =>
        cur === null
          ? null
          : cur.kind === "history"
            ? { ...cur, sessions: cur.source === "scope" ? sessionsForScope(sessions, cur.filter) : sessions }
            : cur,
      );
    });
  // Workspace: open the folder chooser and switch projects. The hook resets the
  // transcript and refreshes meta on a pick. A cancel is a no-op.
  const refreshTabsAfterMutation = useCommittedCommand((latest: () => boolean) => (
    refreshTabMetas(latest, { afterMutation: true })
  ));
  const switchFolder = useCommittedCommand(async (path?: string) => {
    enterConversation();
    return lazyNavigateWorkspace(path, {
      claimIntent: noteNavigationIntent,
      beginSurface: beginNavigationSurface,
      isIntentCurrent: isNavigationIntentCurrent,
      pickWorkspace,
      switchWorkspace,
      markProjectChanged: setProjectRevision,
      refreshTabsAfterMutation,
      maskTarget: settleNavigationSurface,
    });
  });

  const { topicTitleDraft, topicbarEditing, setTopicTitleDraft, startActiveTopicRename,
    cancelActiveTopicRename, commitActiveTopicRename, renameTopic, refreshProjectsAndTabs,
    onCreateTopic, onCreateIsolatedWorktree, onAddProject } = useProjectTopicCommands({
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
  const sidebarNavTooltipDisabled = !sidebarCollapsed;
  const browserPreviewChrome = typeof window !== "undefined" && !window.runtime;
  const browserMockScenario = browserPreviewChrome ? browserMockScenarioParam() : "";
  const guidanceQueueMockItems = isGuidanceMockScenario(browserMockScenario) ? GUIDANCE_QUEUE_MOCK_ITEMS : undefined;
  const workspacePanelResetWidth = desktopLayoutStyle === "creation"
    ? defaultCreationRightDockTreeWidth()
    : defaultRightDockTreeWidth();
  const workspacePanelResizeMinWidth = workspacePanelAriaMinWidth(workspacePanelMinWidth, workspacePanelRenderWidth);
  const workspacePanelResizeMaxWidth = workspacePanelAvailableWidth;
  const sidebarCreation = desktopLayoutStyle === "creation";
  // Command palette shortcut label (⌘K / Ctrl+K), platform-aware.
  const commandPaletteShortcut = formatShortcutCombo(
    resolvedShortcutCombo("commandPalette.open", desktopPlatform),
    desktopPlatform,
  );
  // Dock collapse/expand toggle. Rendered in the dock's own tools row when the
  // dock is open (its top-right corner), and in the topic bar when closed.
  const dockToggleButton = (
    <Tooltip label={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}>
      <button
        className={[
          "topicbar__chrome-btn",
          "topicbar__chrome-btn--workspace",
          surfaceWorkspacePanelRenderable ? "topicbar__chrome-btn--active" : "",
        ].filter(Boolean).join(" ")}
        type="button"
        onClick={toggleWorkspacePanel}
        aria-label={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
        aria-pressed={surfaceWorkspacePanelRenderable}
      >
        <PanelRight size={15} />
      </button>
    </Tooltip>
  );
  const topicbarTitle = sidebarImDetailConnection ? t("botDetail.title", { name: sidebarImDetailConnection.title }) : topicDisplayTitle(activeTab);
  const topicbarWorkspaceLabel = sidebarImDetailConnection ? t("botDetail.subtitle") : activeTab ? tabWorkspaceTitle(activeTab) : "";
  const topicbarWorkspacePath = activeTab?.scope === "project" ? activeTab.workspaceRoot || state.meta?.cwd : "";
  const topicbarImSource = activeTab?.scope === "global" && activeTab.topicId ? imTopicSources[activeTab.topicId] : undefined;
  const topicbarImSourceLabel = sidebarImDetailConnection
    ? sidebarImDetailConnection.platformLabel
    : topicbarImSource ? t("msg.fromIm", { source: topicbarImSource.label }) : "";
  const topicbarImSourcePlatform = sidebarImDetailConnection?.platform ?? topicbarImSource?.platform;
  const topicbarSubtitleVisible = !sidebarCreation && Boolean(activeTab?.isolatedWorktree || topicbarImSourceLabel);
  const topicbarSubtitleTitle = sidebarImDetailConnection
    ? [topicbarWorkspaceLabel, topicbarImSourceLabel, sidebarImScopeLabel(sidebarImDetailConnection, t)].filter(Boolean).join(" · ")
    : [topicbarWorkspacePath || topicbarWorkspaceLabel, topicbarImSourceLabel].filter(Boolean).join(" · ");
  const topicbarCanRename = !sidebarImDetailConnection && (Boolean(activeTab?.topicId) || Boolean(activeTab?.remote));
  const topicbarTitleEditSize = Math.min(56, Math.max(4, topicTitleDraft.length || topicbarTitle.length || 1));
  const sidebarWorkbench = desktopLayoutStyle === "workbench";
  // The Wails drag runtime ignores anything with detail !== 1, so a double click
  // on a --wails-draggable region never reaches the OS. Both platforms that hide
  // their native title bar need this handled here.
  const chromeDoubleClickZooms = windowsFramelessChrome || desktopPlatform === "darwin";
  const handleChromeTitlebarDoubleClick = useCommittedCommand((event: ReactMouseEvent<HTMLDivElement>) => {
    if (!chromeDoubleClickZooms) return;
    const target = event.target as HTMLElement | null;
    const onChromeSurface = target?.closest(".app-chrome, .topicbar, .workbench-dock__tools, .management-screen__chrome");
    const onMacOSWorkbenchSidebarTitlebar = isMacOSWorkbenchSidebarTitlebar(target, event.clientY, desktopPlatform);
    if (!onChromeSurface && !onMacOSWorkbenchSidebarTitlebar) return;
    if (target?.closest("button, input, textarea, select, a, [role='button'], [role='tab'], .windows-window-controls")) return;
    event.preventDefault();
    void nativeWindowCommands.toggleMaximize().then(syncMainWindowMaximised).catch(() => undefined);
  });
  const minimizeMainWindow = useCommittedCommand(() => { void nativeWindowCommands.minimize(); });
  const toggleMainWindowMaximized = useCommittedCommand(() => {
    void nativeWindowCommands.toggleMaximize().then(syncMainWindowMaximised).catch(() => undefined);
  });
  const closeMainWindow = useCommittedCommand(() => { void nativeWindowCommands.close(); });
  const closeSettings = useCommittedCommand(() => {
    setSettingsFocus(null);
    setSettingsTarget(null);
  });
  const handleSettingsChanged = useCommittedCommand((settings?: SettingsView | null) => {
    void refreshMeta();
    void refreshProviderSetupState().catch(() => {});
    void reloadDesktopPreferences(settings);
  });
  const { completeOnboarding, chooseOnboardingProvider, skipOnboarding } = useOnboardingCommands(() => setProviderSetupNeeded(false));
  const openSidebarSettings = useCommittedCommand((tab: import("./lib/types").SettingsTab) => {
    closeTransientOverlays();
    setSettingsTarget(tab);
  });
  const toggleSidebarSearch = useCommittedCommand(() => {
    setSidebarSearchOpen((open) => !open);
    setSidebarSearchFocusSignal((signal) => signal + 1);
  });
  const handleWorktreeMerged = useCommittedCommand(async (result: WorktreeMergeResult) => {
    const tabToClose = worktreeMergeTabId;
    if (!tabToClose || !result.sourceRoot || !result.worktreeRoot || !result.targetBranch || !result.mergedCommit || !result.worktreeBranch || !result.worktreeHead) {
      throw new Error(result.error || t("worktree.mergeReceiptInvalid"));
    }
    const navigationIntentSeq = noteNavigationIntent();
    try {
      const navigationIntentToken = await registeredNavigationIntent(navigationIntentSeq);
      if (!navigationIntentToken || !isNavigationIntentCurrent(navigationIntentSeq)) {
        showToast(t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 });
        return;
      }
      const lifecycle = await runWorktreeMergeLifecycle(result, tabToClose, navigationIntentToken, {
        ensureSource: (sourceRoot) => singleSurfaceLayout
          ? ensureBlankSurface("project", sourceRoot, navigationIntentSeq)
          : ensureBlankTab("project", sourceRoot, navigationIntentSeq),
        isNavigationCurrent: () => isNavigationIntentCurrent(navigationIntentSeq),
        seedSource: seedActiveTabMeta,
        listTabs: () => app.ListTabs(),
        closeWorktree: (request) => app.CloseMergedWorktreeTab(request),
        finalize: (request) => app.FinalizeWorktreeMerge(request),
        onNavigationPreserved: () => showToast(t("worktree.navigationChangedPreserved"), "error", { durationMs: 9000 }),
        onCloseBlocked: () => showToast(t("worktree.cleanupViewBlocked"), "error", { durationMs: 8000 }),
      });
      if (lifecycle.phase === "finalized") showWorktreeCleanupNotice(lifecycle.cleanup, t, showToast);
    } catch (caught: unknown) {
      showToast(`${t("worktree.mergeDoneCleanupFailed")} ${caught instanceof Error ? caught.message : String(caught)}`, "error", { durationMs: 9000 });
    }
  });
  // Creation keeps the classic sidebar/chat structure while gating chrome tweaks
  // behind its own style flag so classic/workbench remain unchanged.
  const appChromeHidden = sidebarWorkbench || sidebarCreation;
  const workbenchChromeHidden = sidebarWorkbench;
  const sidebarClassName = [
    "sidebar",
    sidebarCollapsed ? "sidebar--collapsed" : "",
    sidebarWorkbench ? "sidebar--workbench" : "",
  ].filter(Boolean).join(" ");

  const footerTodo = showTodos ? {
    identity: scopedTodoBatch,
    props: {
      stateKey: scopedTodoBatch,
      todos,
      running: visibleRuntimeState.running,
      pendingPrompt: visibleRuntimeState.pendingPrompt,
      onContinue: activeTabId && !activeTab?.readOnly && (remoteSurfaceActive ? remoteComposerReady : controllerReady)
        ? handleTodoContinue
        : undefined,
      onDismiss: dismissTodos,
    },
  } : undefined;
  const footerUndo = rewindState ? {
    identity: `${activeTabId ?? ""}:${rewindState.transactionId ?? "rewind"}`,
    props: {
      meta: {
        turns: rewindState.turnDiff,
        filesRestored: rewindState.filesRestored ?? [],
        filesRemoved: rewindState.filesRemoved ?? [],
        onUndo: () => {
          const tabId = activeTabId;
          if (!tabId) return;
          const tx = rewindState.transactionId;
          const undoTabId = rewindState.undoTabId || tabId;
          const undo = tx && rewindState.undoAvailable ? undoRewindForTab(undoTabId, tx) : Promise.resolve(true);
          void undo.then((ok) => {
            if (!ok) return;
            setRewindStateForTab(tabId, null);
            setComposerInsertRequestsByTab((current) => ({
              ...current,
              [tabId]: { id: Date.now(), text: "", mode: "replace" },
            }));
            setRewindSignal((value) => value + 1);
            setDockRefreshKey((value) => value + 1);
            setProjectRevision((value) => value + 1);
          });
        },
      },
    },
  } : undefined;
  let decisionFooterSurface: DecisionFooterSurface | undefined;
  if ((visibleDecisionSurface === "tool_approval" || visibleDecisionSurface === "plan_approval") && state.approval) {
    decisionFooterSurface = {
      kind: "approval",
      identity: `${activeTabId ?? ""}:${state.approval.id}`,
      props: {
        approval: state.approval,
        cwd: state.meta?.cwd,
        tabId: activeTabId,
        workspaceScopeKey,
        insertRequest: activePlanRevisionInsertRequest,
        onRevisionActiveChange: handleRevisionActiveChange,
        onAnswer: handleApprovalAnswer,
        onResolveRecovery: handleRecoveryAnswer,
        onRevisePlan: handleRevisePlan,
        onExitPlan: handleExitPlan,
        onStop: () => void handleCancelActive(),
        toolApprovalMode,
      },
    };
  } else if (visibleDecisionSurface === "ask" && state.ask) {
    decisionFooterSurface = {
      kind: "ask",
      identity: `${activeTabId ?? ""}:${state.ask.id}`,
      props: {
        ask: state.ask,
        onAnswer: handleQuestionAnswer,
        onDismiss: handleQuestionDismiss,
        onStop: () => void handleCancelActive(),
      },
    };
  } else if (visibleDecisionSurface === "mcp_interaction" && state.mcpInteraction) {
    decisionFooterSurface = {
      kind: "mcp",
      identity: `${activeTabId ?? ""}:${state.mcpInteraction.id}`,
      props: {
        interaction: state.mcpInteraction,
        busy: false,
        onAnswer: handleMCPAnswer,
        onOpenLink: openExternal,
      },
    };
  } else if (visibleDecisionSurface === "extension_form" && state.extensionForm) {
    decisionFooterSurface = {
      kind: "extension",
      identity: `${activeTabId ?? ""}:${state.extensionForm.pluginId}:${state.extensionForm.surfaceId}`,
      props: {
        surface: state.extensionForm,
        busy: extensionFormBusy,
        onSubmit: (values) => void submitExtensionForm(values),
        onCancel: () => void cancelExtensionForm(),
      },
    };
  } else if (visibleDecisionSurface === "workspace_conflict" && workspaceConflict) {
    decisionFooterSurface = {
      kind: "runtime",
      identity: "workspace-conflict",
      props: {
        id: "workspace-conflict",
        title: t("runtime.workspaceConflictTitle"),
        badge: t("runtime.workspaceConflictBadge"),
        meta: workspaceConflict.state === "local"
          ? t("runtime.workspaceConflictLocal", { title: workspaceConflict.ownerTitle || t("runtime.unknownTask"), label: workspaceConflict.ownerLabel || t("workspace.title") })
          : t("runtime.workspaceConflictExternal"),
        note: t("runtime.workspaceConflictNote"),
        onCancel: cancelWorkspaceConflict,
        actions: [
          ...(workspaceConflict.canReveal ? [{
            key: "1", label: t("runtime.revealWriter"), description: t("runtime.revealWriterDesc"),
            onClick: () => void revealWorkspaceWriter(),
          }] : []),
          ...(workspaceConflict.canCreateWorktree ? [{
            key: "2", label: t("runtime.openWorktree"), description: t("runtime.openWorktreeDesc"),
            onClick: () => void continueInDeliveryWorktree(),
          }] : []),
        ],
        secondaryAction: {
          key: "Esc", label: t("runtime.cancelWait"), description: t("runtime.cancelWaitDesc"),
          onClick: cancelWorkspaceConflict,
        },
      },
    };
  } else if (visibleDecisionSurface === "close_active" && pendingClose) {
    decisionFooterSurface = {
      kind: "runtime",
      identity: "close-active",
      props: {
        id: "close-active",
        title: t("runtime.closeTitle"),
        badge: t("status.jobs", { n: pendingClose.work.jobs.length }),
        meta: t("runtime.closeMeta"),
        onCancel: () => setPendingClose(null),
        actions: [
          {
            key: "1", label: t("runtime.keepRunning"), description: t("runtime.keepRunningDesc"),
            onClick: () => void resolvePendingClose("keep_running"), disabled: pendingClose.stopping,
          },
          {
            key: "2", label: pendingClose.stopping ? t("status.jobStopping") : t("runtime.stopAndClose"),
            description: t("runtime.stopAndCloseDesc"), onClick: () => void resolvePendingClose("stop_and_close"),
            danger: true, disabled: pendingClose.stopping,
          },
        ],
        secondaryAction: {
          key: "Esc", label: t("runtime.returnToTask"), description: t("runtime.closeCancelDesc"),
          onClick: () => setPendingClose(null), disabled: pendingClose.stopping,
        },
      },
    };
  } else if (visibleDecisionSurface === "clear_context") {
    decisionFooterSurface = {
      kind: "clear-context",
      identity: "clear-context",
      props: { onCancel: cancelClearContext, onConfirm: () => void confirmClearContext() },
    };
  }

  return (
    <ShellExpandProvider>
    <RemoteNavigationContext.Provider value={openRemoteProject}>
    <UpdaterProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
    <WindowChromeLifecycle />
    <StartupGateLifecycle />
    <AppRuntimeEffects
      running={state.running}
      onEvent={handleRuntimeEvent}
      onReady={handleRuntimeReady}
      onRebuilt={handleRuntimeRebuilt}
      onRemoteStatus={handleRemoteStatus}
      onRemoteForwards={handleRemoteForwards}
      onRemoteServer={handleRemoteServer}
      onInitialRemoteHosts={handleInitialRemoteHosts}
      onInitialRemoteStatuses={handleInitialRemoteStatuses}
    />
      <div
        ref={appRef}
        onDoubleClickCapture={handleChromeTitlebarDoubleClick}
        className={[
        "app",
        `app--${desktopPlatform}`,
        windowsFramelessChrome ? "app--windows-frameless" : "",
        browserPreviewChrome ? "app--browser-preview" : "",
        sidebarWorkbench ? "app--workbench" : "",
        sidebarCreation ? "app--creation" : "",
        !sidebarWorkbench && !sidebarCreation ? "app--classic" : "",
      ].filter(Boolean).join(" ")}
    >
      <ThemeBackground />
      {sidebarWorkbench && <div className="app__dock-toggle" inert={managementActive}>{dockToggleButton}</div>}
      <div
        ref={layoutRef}
        className={[
          "layout",
          sidebarWorkbench ? "layout--workbench" : "",
          workbenchChromeHidden ? "layout--workbench-chrome-hidden" : "",
          sidebarCreation ? "layout--creation-chrome-hidden" : "",
          sidebarImDetailConnection ? "layout--statusbar-hidden" : "",
          sidebarCollapsed ? "layout--sidebar-collapsed" : "",
          sidebarResizing ? "layout--resizing layout--sidebar-resizing" : "",
          surfaceWorkspacePanelGridOpen ? "layout--workspace-open" : "",
          workspacePanelOverlay ? "layout--workspace-overlay" : "",
          "layout--terminal-drawer-open",
          terminalSurfaceOpen ? "layout--terminal-drawer-expanded" : "",
          terminalResizing ? "layout--terminal-resizing" : "",
          workspacePanelOpen && workspacePanelMaximized ? "layout--workspace-maximized" : "",
          workspacePanelResizing ? "layout--resizing layout--workspace-resizing" : "",
        ]
          .filter(Boolean)
          .join(" ")}
        style={layoutStyle}
      >
        {!appChromeHidden && (
          <AppChrome
            platform={desktopPlatform}
            browserPreviewChrome={browserPreviewChrome}
            workbenchChrome={sidebarWorkbench}
            tabs={visibleTabs}
            activeTabId={visibleTabId}
            revealActiveSignal={tabRevealSignal}
            commandCompact={true}
            sidebarTogglePressed={sidebarTogglePressed}
            sidebarExpandBlocked={sidebarExpandBlocked}
            sidebarCollapsed={sidebarCollapsed}
            sidebarToggleTitle={sidebarToggleTitle}
            workspacePanelMaximized={workspacePanelMaximized}
            workspacePanelRenderable={surfaceWorkspacePanelRenderable}
            workspacePanelLabel={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
            onToggleSidebar={toggleSidebar}
            onToggleWorkspacePanel={toggleWorkspacePanel}
            onTabChange={(id) => void handleTabChange(id)}
            onTabClose={(id) => void handleTabClose(id)}
            onTabsClose={(ids, nextActiveTabId) => void handleTabsClose(ids, nextActiveTabId)}
            onTabsReorder={(ids) => void handleTabsReorder(ids)}
            onNewTab={() => void handleNewTab()}
            onOpenPalette={() => void openPalette()}
          />
        )}
        <a className="skip-to-composer" href="#composer-input">
          {t("shortcuts.skipToComposer")}
        </a>

        <SidebarRegion
          automation={page.kind === "automation"}
          className={sidebarClassName}
          workbench={sidebarWorkbench}
          creation={sidebarCreation}
          collapsed={sidebarCollapsed}
          navTooltipDisabled={sidebarNavTooltipDisabled}
          searchOpen={sidebarSearchOpen}
          togglePressed={sidebarTogglePressed}
          toggleTitle={sidebarToggleTitle}
          t={t}
          onNewSession={() => void handleNewTab()}
          onOpenTrash={() => void openTrash()}
          onOpenAutomation={() => openPage({ kind: "automation" })}
          onOpenSettings={openSidebarSettings}
          onToggleSearch={toggleSidebarSearch}
          onToggle={toggleSidebar}
          resize={{
            min: sidebarResizeMinWidth, max: SIDEBAR_MAX_WIDTH, value: sidebarRenderWidth,
            onPointerDown: startSidebarResize, onKeyDown: resizeSidebarWithKeyboard,
            onReset: () => setExpandedSidebarWidth(desktopLayoutStyle === "creation" ? defaultCreationSidebarWidth() : defaultSidebarWidth()),
          }}
          projectTree={{
            activeScope: activeTab?.scope, activeWorkspaceRoot: activeTab?.workspaceRoot,
            activeTopicId: activeTab?.topicId, activeSessionPath: activeTab?.sessionPath,
            activeRemote: activeTab?.remote, imTopicSources, onOpenTopic: handleOpenTopic,
            onCreateTopic, onCreateIsolatedWorktree,
            onTopicsChanged: refreshProjectsAndTabs, onRenameTopic: renameTopic, refreshSignal: projectRevision,
            onAddProject,
            timeFilter: topicTimeFilter, onTimeFilterChange: setTopicTimeFilter,
            variant: sidebarWorkbench ? "workbench" : sidebarCreation ? "creation" : "classic",
            searchExpanded: !sidebarCreation || sidebarSearchOpen, searchFocusSignal: sidebarSearchFocusSignal,
            showShortcutBadges: showTopicBadges, shortcutPlatform: desktopPlatform,
            onVisibleTopicsChange: handleVisibleTopicsChange,
          }}
        />

        <section className={`chat-pane${creationEmptyHero ? " chat-pane--creation-empty" : ""}`}>
          <TopicbarRegion view={{
            automationReturn, automationReturnLabel: locale === "en" ? "Back to automation" : locale === "zh-TW" ? "返回自動化" : "返回自动化",
            chromeHidden: workbenchChromeHidden,
            sidebar: { title: sidebarToggleTitle, blocked: sidebarExpandBlocked, pressed: sidebarTogglePressed, collapsed: sidebarCollapsed },
            title: { text: topicbarTitle, hover: !topicbarCanRename && sidebarImDetailConnection ? topicbarTitle : topicTitle(activeTab),
              renameLabel: t("topicBar.renameSession"), editing: topicbarEditing, draft: topicTitleDraft,
              editSize: sidebarCreation ? topicbarTitleEditSize : undefined, canRename: topicbarCanRename, workspaceLabel: topicbarWorkspaceLabel },
            subtitle: { visible: topicbarSubtitleVisible, title: topicbarSubtitleTitle, worktreeTabId: activeTab?.isolatedWorktree ? activeTab.id : undefined,
              mergeLabel: t("worktree.mergeAction"), mergeTooltip: t("worktree.mergeButtonTooltip"),
              sourcePlatform: topicbarImSourcePlatform, sourceLabel: topicbarImSourceLabel },
          }} commands={{
            openAutomation: () => openPage({ kind: "automation" }), toggleSidebar,
            setTitleDraft: setTopicTitleDraft, commitRename: commitActiveTopicRename, cancelRename: cancelActiveTopicRename,
            startRename: startActiveTopicRename, openWorktree: setWorktreeMergeTabId,
          }}>
            <div className="topicbar__actions">
              <Tooltip label={`${t("shortcuts.action.commandPalette")} ${commandPaletteShortcut}`}>
                <button
                  className="topicbar__action-btn topicbar__action-btn--icon topicbar__action-btn--utility"
                  type="button"
                  aria-label={t("shortcuts.action.commandPalette")}
                  onClick={() => void openPalette()}
                >
                  <Search size={15} />
                </button>
              </Tooltip>
              <TopicbarActionsRegion sessionIdentity={activeTab?.id}
                external={shouldMountExternalOpener(activeTab, Boolean(sidebarImDetailConnection)) && activeTab
                  ? { tabId: activeTab.id, dismissSignal: transientOverlayDismissSignal } : undefined}
                session={!sidebarImDetailConnection ? {
                  sessionHasContent, getSessionMarkdown, exportSession: (format) => void exportSession(format),
                  toggleTerminal: toggleTerminalPanel, terminalEnabled: !remoteSurfaceActive,
                  terminalOpen: terminalPanelOpen && !remoteSurfaceActive, prefetchTerminal: prefetchTerminalPanel,
                  openSessionSummary: () => setTasksOpen((open) => open ? false : "session"), tasksOpen: Boolean(tasksOpen),
                } : undefined}
              />
              {sidebarCreation && dockToggleButton}
              {tasksOpen && (
                <div className="taskmonitor-popover" role="dialog" aria-label={t("summary.session")}>
                  <Suspense fallback={null}>
                    <TaskMonitorPanel
                      key={`${activeTab?.id || activeTabId || "none"}:${activeTab?.workspaceRoot || "global"}:${activeTab?.sessionPath || ""}:${tasksOpen}`}
                      tabID={activeTab?.id || activeTabId || ""}
                      initialOpen initialScope={tasksOpen || "session"}
                      popover
                      summaryMode
                      onClose={() => setTasksOpen(false)}
                      onOpenSession={openTaskMonitorSession}
                    />
                  </Suspense>
                </div>
              )}
            </div>
          </TopicbarRegion>

          <SessionStatusBanners
            t={t}
            takenOver={Boolean(activeTab?.takenOver)}
            reclaimTabId={activeTab?.id ?? ""}
            reclaimBusyTabId={reclaimBusyTab}
            onReclaim={reclaimSession}
            leaseBlocked={leaseBlockedTab ? { tabId: leaseBlockedTab.id, message: leaseBlockedTab.runtime!.issue!.message } : null}
            startupError={state.meta?.startupErr}
            takeoverDialogTabId={takeoverDialogTab}
            onOpenTakeover={openTakeoverDialog}
            onCloseTakeover={closeTakeoverDialog}
            configWarnings={configLoadWarnings}
            onOpenConfigFile={openConfigFile}
            onReloadConfigFile={reloadConfigFile}
            onDismissConfigWarnings={dismissConfigWarnings}
            providerSetupNeeded={providerSetupNeeded}
            needsOnboarding={needsOnboarding}
            onConfigureProvider={() => {
              setProviderSetupNeeded(false);
              chooseOnboardingProvider();
            }}
            updateChecksEnabled={startupUpdateChecksEnabled === true}
            onShowReleaseNotes={showReleaseNotes}
          />

          <main className="main">
            {sidebarImDetailConnection && !runtimeTransitioning ? (
              <SidebarImConnectionDetail
                connection={sidebarImDetailConnection}
                onClose={() => setSidebarImDetailConnectionId("")}
                onOpenSettings={openBotSettings}
                onManageAllowlist={() => openBotAllowlistSettings(sidebarImDetailConnection.connectionId)}
                onOpenSession={() => void openSidebarImConnectionSession(sidebarImDetailConnection)}
              />
            ) : noticePreviewMockEnabled() ? (
              <NoticePreviewPanel />
            ) : activeTab?.remote ? (
              <Suspense fallback={null}><RemoteSessionSurface tab={activeTab} session={remoteSession}
                surfaceCommitToken={surfaceCommitToken} onSurfacePaintReady={handleSurfacePaintReady} /></Suspense>
            ) : (
              <>
                <div className="transcript-navigation-surface" aria-busy={runtimeTransitioning}>
                  <div
                    className="transcript-navigation-content"
                    aria-hidden={runtimeTransitioning || undefined}
                    ref={(node) => {
                      if (!node) return;
                      (node as HTMLElement & { inert?: boolean }).inert = runtimeTransitioning;
                    }}
                  >
                    <Transcript
                      items={visibleTranscriptItems}
                      live={runtimeTransitioning ? undefined : state.live}
                      liveStore={liveStore}
                      tabId={visibleTranscriptTabId}
                      geometrySessionKey={visibleTranscriptGeometryKey}
                      footerHeight={footerHeight}
                      onPrompt={handleTranscriptPrompt}
                      onDeliveryContinue={() => void handleDeliveryContinue()}
                      onAcceptDelivery={() => void app.AcceptDeliveryToTab(activeTabIdRef.current ?? "")}
                      onOpenChanges={() => openRightDockMode("changed")}
                      onOpenVerification={openTurnVerification}
                      onEditPrompt={handleEditPrompt}
                      onRewind={handleMessageAction}
                      checkpoints={state.checkpoints}
                      actionPending={state.messageAction != null}
                      rewindDisabled={Boolean(activeTab?.readOnly) || !controllerReady || hydratePlaceholderActive || rewindState != null || rewindCommitting || state.running || state.messageAction != null || state.approval != null || state.ask != null || clearContextPending || runtimeTransitioning}
                      running={state.running || rewindCommitting}
                      turnStartAt={state.turnStartAt}
                      contentRevision={state.historyLayoutRevision}
                      historyMutation={state.historyMutation}
                      welcomeVariant={sidebarCreation ? "creation" : "default"}
                      creationMode={sidebarCreation}
                      actionHoverMenus={sidebarCreation && !hydratePlaceholderActive && !runtimeTransitioning}
                      rewindSignal={rewindSignal}
                      revealSignal={transcriptRevealSignal}
                      hydrating={transcriptHydrating || (runtimeTransitioning && !navigationTargetDataReady)}
                      hasOlderHistory={!runtimeTransitioning && state.historyHasOlder && !rewindState}
                      historyStartTurn={state.historyStartTurn}
                      historyTotalTurns={state.historyTotalTurns}
                      loadingOlderHistory={state.historyOlderLoading}
                      olderHistoryError={state.historyOlderError}
                      onLoadOlderHistory={handleLoadOlderHistory}
                      invocationMetadata={visibleTranscriptTabId ? invocationMetadataByTab[visibleTranscriptTabId] : undefined}
                      surfaceCommitToken={surfaceCommitToken}
                      onSurfacePaintReady={handleSurfacePaintReady}
                    />
                  </div>
                  {runtimeTransitioning ? (
                    <div className="transcript-navigation-overlay" role="status" aria-live="polite">
                      <span className="transcript-navigation-overlay__spinner" aria-hidden="true" />
                      <span>{t("common.loading")}</span>
                    </div>
                  ) : null}
                </div>
                {!runtimeTransitioning && state.hydrateError ? <div className="history-load-error" role="alert"><span>{state.hydrateError}</span><button type="button" className="btn btn--small" onClick={() => void retrySessionHistory(activeTabId)}>{t("common.retry")}</button></div> : null}
              </>
            )}
          </main>

          <DecisionFooterRegion
            hidden={Boolean(sidebarImDetailConnection)}
            className={["footer", terminalSurfaceOpen && !sidebarCreation ? "footer--compact" : "", visibleDecisionSurface ? "footer--decision" : "", runtimeTransitioning ? "footer--navigation-hidden" : ""].filter(Boolean).join(" ")}
            footerRef={footerRef}
            style={navigationSurface?.phase === "source-retained" && footerHeight > 0 ? { height: footerHeight, minHeight: footerHeight, boxSizing: "border-box" } : undefined}
            todo={footerTodo}
            undo={footerUndo}
            decision={decisionFooterSurface}
            composer={{
              hidden: composerSurfaceHidden,
              inert: runtimeTransitioning,
              hero: creationEmptyHero,
              headline: t("welcome.creation.title"),
              props: {
                ...conversationView.composer,
                running: conversationView.composer.running || (!remoteSurfaceActive && rewindCommitting),
                collaborationMode,
                toolApprovalMode,
                qualityFloor: composerProfile.qualityFloor,
                floorInferred: (activeTab?.floorInferred ?? false) && !composerProfile.pending.qualityFloor,
                onSetQualityFloor: applyQualityFloor,
                goal,
                tabId: activeTabId,
                onSend: remoteSurfaceActive ? remoteComposerSend : handleSend,
                onInvocationMetadataChange: handleInvocationMetadataChange,
                onSteer: handleSteer,
                onCancel: remoteSurfaceActive ? remoteCancel : handleCancelActive,
                onCycleMode: cycleMode,
                onSetMode: applyMode,
                onSetCollaborationMode: setCollaborationModeFromUi,
                onSetToolApprovalMode: applyToolApprovalMode,
                onToggleYoloApprovalMode: toggleYoloApprovalMode,
                onClearGoal: clearGoalFromUi,
                onPauseGoal: pauseGoalFromUi,
                onResumeGoal: resumeGoalFromUi,
                onSwitchModel: switchModelFromUi,
                onSetEffort: setEffortFromUi,
                insertRequest: composerInsertRequest,
                selectedTextRequest,
                readOnly: Boolean(activeTab?.readOnly),
                disabled: runtimeTransitioning || rewindCommitting || state.messageAction != null || Boolean(decisionSurface),
                submitDisabled: remoteSurfaceActive ? !remoteComposerReady || !remoteComposerProfileReady : !controllerReady,
                decisionPending: rewindCommitting || state.messageAction != null || Boolean(decisionSurface),
                ready: remoteSurfaceActive ? remoteComposerReady && remoteComposerProfileReady : controllerReady,
                liveStore: remoteSurfaceActive ? remoteSession.liveStore : liveStore,
                suspendedByDecision: Boolean(decisionSurface),
                transientDismissSignal: transientOverlayDismissSignal,
                sessionKey: composerSessionKey,
                workspaceScopeKey,
                fileRefRefreshKey: composerFileRefRefreshKey,
                guidanceConsumedKey: latestGuidanceConsumed?.key,
                guidanceConsumedItemId: latestGuidanceConsumed?.itemId,
                guidanceConsumedText: latestGuidanceConsumed?.text,
                guidanceQueuePreviewItems: guidanceQueueMockItems,
                showContextWindowRing: sidebarCreation,
                heroMode: creationEmptyHero,
              },
            }}
          />
        </section>

        <WorkspaceDockRegion
          visible={surfaceWorkspacePanelRenderable}
          overlay={surfaceWorkspacePanelOverlay}
          mode={rightDockMode}
          creation={sidebarCreation}
          remoteAvailable={remoteHosts.length > 0}
          showContext={SHOW_CONTEXT_DOCK}
          t={t}
          onMode={openRightDockMode}
          onRemote={openRemoteDock}
          remote={{ onClose: closeWorkspacePanel }}
          context={{
            ...conversationView.context, sessionTurns,
            refreshKey: dockRefreshKey + visibleRuntimeState.contextPanelSeq,
          }}
          workspaceKey={workspaceTreeMemoryKey}
          workspace={{
            open: surfaceWorkspacePanelRenderable, tabId: activeTabId, cwd: state.meta?.cwd,
            workspaceScopeKey, workspaceMemoryKey: workspaceTreeMemoryKey,
            dockTreeWidth: rightDockTreeWidth, dockPreviewWidth: rightDockPreviewWidth,
            onRestoreDockWidths: restoreWorkspaceDockWidths, maximized: workspacePanelMaximized,
            panelWidth: workspacePanelRenderWidth, onClose: closeWorkspacePanel,
            onToggleMaximized: toggleWorkspaceMaximized,
            onPreviewModeChange: handleWorkspacePreviewModeChange, onAddToChat: addWorkspaceTextToComposer,
            onAddCodeToChat: addWorkspaceCodeToComposer, onRequestPanelWidth: ensureWorkspacePanelWidth,
            onFileTreeRefresh: refreshComposerFileRefs, onSessionRevertCommitted: handleSessionRevertCommitted,
            onOpenInTerminal: remoteSurfaceActive ? undefined : openTerminalForPath,
            initialViewMode: rightDockMode === "changed" ? "changed" : "files",
            completionSummary: state.completionSummary, turnStartAt: state.turnStartAt,
            verificationRevealRequest, qualityFloor: composerProfile.qualityFloor,
            showViewTabs: false, creationMode: sidebarCreation,
          }}
          resizer={surfaceWorkspacePanelGridOpen ? {
            min: workspacePanelResizeMinWidth,
            max: Math.max(workspacePanelResizeMaxWidth, workspacePanelRenderWidth),
            value: workspacePanelRenderWidth,
            onPointerDown: startWorkspacePanelResize,
            onKeyDown: resizeWorkspacePanelWithKeyboard,
            onReset: () => setSavedWorkspacePanelWidth(workspacePanelResetWidth),
          } : undefined}
        />
        <AppBottomRegions
          terminal={{
            surfaceVisible: chatSurfaceVisible,
            open: terminalSurfaceOpen, contentVisible: terminalContentVisible,
            remoteSurface: remoteSurfaceActive, t,
            panel: {
              tabId: activeTabId ?? "", cwd: state.meta?.cwd, readOnly: Boolean(activeTab?.readOnly),
              open: terminalSurfaceOpen, fitEnabled: terminalFitEnabled,
              onClose: closeTerminalPanel,
              onAddOutput: (sessionId) => void addTerminalOutputToComposer(sessionId),
              onAddToChat: addTerminalSelectionToComposer,
            },
            resizer: {
              min: TERMINAL_MIN_HEIGHT, max: terminalResizeMaxHeight,
              value: liveTerminalHeight ?? terminalRenderHeight,
              onPointerDown: startTerminalResize, onKeyDown: resizeTerminalWithKeyboard,
              onReset: () => setSavedTerminalHeight(TERMINAL_DEFAULT_HEIGHT),
            },
          }}
          status={!statusBarVisible ? undefined : {
            ...conversationView.status,
            running: conversationView.status.running || (!remoteSurfaceActive && rewindCommitting),
            onCancelJob: remoteSurfaceActive ? remoteSession.cancelJob : cancelJob,
            onCancelRuntimeJob: cancelRuntimeJob, onRevealRuntime: revealBackgroundRuntime,
            sessionTurns, labelStyle: statusBarStyle, items: statusBarItems, extensionStatuses: extensionStatusList,
            onConnectRemote: connectAndOpenRemoteWorkspace,
            onDisconnectRemote: (hostId) => void app.DisconnectRemoteHost(hostId).catch(() => {}),
            onManageRemote: () => setSettingsTarget("remote"), onOpenRemote: requestRemoteExplorer,
            onOpenRemoteWorkspace: openRemoteWorkspaceFromStatus, remoteHosts, remoteStatuses,
          }}
        />
      </div>

      <AppOverlayHost
        history={histView ? {
          view: { kind: histView.kind, sessions: histView.sessions, running: state.running },
          commands: {
            onResume: onResumeSession, onPreview: previewSession, onDelete: onDeleteSession,
            onRename: onRenameHistorySession,
            onInspectVersions: requestSessionVersions, onClose: closeHistory,
          },
        } : undefined}
        trash={visitedTrash ? { view: { active: page.kind === "trash" },
          commands: { onBack: returnToWorkspace, list: listTrashedSessions, restore: restoreSession, purge: purgeTrashedSession } } : undefined}
        automation={visitedAutomation ? { view: { active: page.kind === "automation" },
          commands: { onBack: returnToWorkspace, onOpenTopic: openAutomationTopic } } : undefined}
        recovery={{
          view: { sessions: histView?.sessions },
          commands: { onResumeSession, onRecoveryCreated, onLineageChanged: onRecoveryLineageChanged },
        }}
        settings={settingsTarget ? {
          view: {
            initialTab: settingsTarget, initialFocus: settingsFocus ?? undefined,
            agentRunning: state.running, desktopPlatform,
            activeWorkspaceKey: `${activeTab?.id ?? activeTabId ?? ""}\u0000${activeTab?.workspaceRoot ?? activeTab?.cwd ?? state.meta?.cwd ?? ""}`,
          },
          commands: { onUseSubagent: prefillSubagentCommand, onClose: closeSettings, onNavigate: setSettingsTarget, onChanged: handleSettingsChanged },
        } : undefined}
        palette={paletteOpen ? {
          view: { open: true, items: paletteItems, placeholder: t("palette.placeholder"), emptyText: t("palette.empty") },
          commands: { onClose: () => setPaletteOpen(false) },
        } : undefined}
        shortcuts={{
          view: { open: shortcutsOpen, platform: desktopPlatform, t },
          commands: { onClose: () => setShortcutsOpen(false) },
        }}
        startup={startupSplashVisible ? {
          view: { hold: startupSplashHold }, commands: { onDone: () => setStartupSplashVisible(false) },
        } : undefined}
        onboarding={needsOnboarding ? {
          view: {}, commands: { onComplete: completeOnboarding, onChooseProvider: chooseOnboardingProvider, onSkip: skipOnboarding },
        } : undefined}
        selection={{
          view: {
            enabled: Boolean(activeTabId && !activeTab?.readOnly && !decisionSurface && !sidebarImDetailConnection && !hydratePlaceholderActive),
            resetKey: activeTabId ?? "",
          },
          commands: { onAddToChat: addSelectedTextToComposer },
        }}
        worktree={worktreeMergeTabId ? {
          view: { tabId: worktreeMergeTabId, isOpen: true },
          commands: { onClose: () => setWorktreeMergeTabId(null), onMerged: handleWorktreeMerged },
        } : undefined}
      />
      {windowsFramelessChrome && (
        <WindowsWindowControls
          maximised={mainWindowMaximised}
          onMinimize={minimizeMainWindow}
          onToggleMaximize={toggleMainWindowMaximized}
          onClose={closeMainWindow}
        />
      )}
    </div>
    </UpdaterProvider>
    </RemoteNavigationContext.Provider>
    </ShellExpandProvider>
  );
}
