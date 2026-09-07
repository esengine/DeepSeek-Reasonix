import { useMemo, useRef, useState, type CSSProperties } from "react";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { useWailsResizeFix } from "../lib/useWailsResizeFix";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import { activeLeaseBlockedTab } from "../lib/tabMetaRefresh";
import { topicTitle } from "../lib/sessionTitles";
import { composerDraftKeyForTab } from "../lib/composerDraftKey";
import { useWindowStatePersistence, useViewportHeightVar } from "../lib/windowState";
import { useManagementWorkspace } from "../lib/useManagementWorkspace";
import { reportPendingRevisionFailure, usePendingPlanRevisions } from "../lib/usePendingPlanRevisions";
import { useComposerModeActions } from "../lib/useComposerModeActions";
import { useRemoteComposerRuntimeActions, useRemoteComposerSend } from "../lib/useRemoteComposerIntegration";
import type { RemoteNavigationCommand } from "../lib/remoteNavigationCommands";
import type { CollaborationMode, TabMeta } from "../lib/types";
import type { RestorableToolApprovalMode } from "../lib/toolApprovalMode";
import type { ComposerProfile, UserPlanModeIntents } from "../lib/composerProfile";
import type { State } from "../lib/useController";
import type { Translator } from "../lib/i18n";
import type { useAppRuntimeAdapter } from "./useAppRuntimeAdapter";
import type { useNavigationSurface } from "../lib/useNavigationSurface";
import { desktopBridge } from "./desktopBridgeAdapter";
import { useSessionOperations } from "./useSessionOperations";
import { useComposerInsertCommands } from "./useComposerInsertCommands";
import { useSessionClearCommands } from "./useSessionClearCommands";
import { useRuntimeStatus } from "./useRuntimeStatus";
import { useAppDiagnostics, useSidebarConnectionValidity } from "./useAppEffectHosts";
import { useActiveTabUiReset, useDecisionSurfaceFocus } from "./useLocalUiLifecycles";
import { useActiveTabMirrorCommit } from "./activeTabMirror";
import { useInvocationMetadata } from "./useInvocationMetadata";
import { useFooterHeightLifecycle } from "./useFooterHeightLifecycle";
import { useNativeSettingsEvent } from "./useNativeSettingsEvent";
import { useWindowsMaximisedSync } from "./useNativeWindowController";
import { useShellGeometry } from "./useShellGeometry";
import { useTopicSummary } from "./useTopicSummary";
import { useComposerProfileProjection } from "./useComposerProfileProjection";
import { useTabBarCommands } from "./useTabBarCommands";
import { useExtensionSurface } from "./useExtensionSurface";
import { useTabProjectionLifecycle } from "./useTabProjectionLifecycle";
import { useSessionUndo } from "./useSessionUndo";
import { useSessionSubmission } from "../lib/useSessionSubmission";
import { useControllerProfileCommands } from "../lib/useControllerProfileCommands";
import { useSessionPromptCommands } from "./useSessionPromptCommands";
import { useSessionControlCommands } from "./useSessionControlCommands";
import { useTodoPanelCommands } from "./useTodoPanelCommands";
import { useSessionExportCommands } from "./useSessionExportCommands";
import { useComposerRouter } from "./useComposerRouter";
import { useComposerGoalCommands } from "./useComposerGoalCommands";
import { useRuntimeEventHandlers } from "./useRuntimeEventHandlers";
import { probeProviderSetupState } from "./StartupGateLifecycle";
import { useSessionBannerCommands } from "./useSessionBannerCommands";
import { useWorkspacePanelCommands } from "./useWorkspacePanelCommands";
import { useTurnVerificationCommands } from "./useTurnVerificationCommands";
import { useTerminalPanelCommands } from "./useTerminalPanelCommands";
import { useRemoteWorkspaceCommands } from "./useRemoteWorkspaceCommands";
import { useAutomationNavigation } from "./useAutomationNavigation";
import { useDesktopNavigation } from "./useDesktopNavigation";
import { useTranscriptSurfaceProjection } from "./useTranscriptSurfaceProjection";
import { useDeliveryContinueCommands } from "./useDeliveryContinueCommands";
import { projectConversation, projectConversationLayout, projectWorkspaceScopeKey, projectWorkspaceTreeMemoryKey } from "./conversationProjection";
import { projectControllerProfiles, projectVisibleTabs } from "./controllerProfileOwner";
import { projectDecisionSurface, type AppDecisionSurfaceKind } from "./decisionSurfaceProjection";
import { createSubmissionPorts, projectSubmissionResources } from "./desktopSubmissionAdapter";
import type { useAppShellStores } from "./useAppShellStores";

function setRemoteComposerProfileForSessionAction(
  tabId: string,
  mode: CollaborationMode,
  approvalMode: import("../lib/types").ToolApprovalMode,
  goal: string,
) {
  return desktopBridge.setRemoteTabComposerProfile(tabId, mode, approvalMode, goal);
}

const WORKSPACE_RESIZER_WIDTH = 8;

type Runtime = ReturnType<typeof useAppRuntimeAdapter>;
type Shell = ReturnType<typeof useAppShellStores>;
type Surface = ReturnType<typeof useNavigationSurface>;
type LiveStore = Runtime["snapshot"]["liveStore"];

export type AppSessionCompositionInput = {
  runtime: Runtime;
  t: Translator;
  showToast: (message: string, level?: "info" | "warn" | "error", options?: { durationMs?: number }) => void;
  shell: Shell;
  core: {
    state: State;
    liveStore: LiveStore;
    activeTabId: string | undefined;
    notice: Runtime["snapshot"]["notice"];
    activeTab: TabMeta | undefined;
    remoteSurfaceActive: boolean;
    remoteSession: RemoteSessionApi;
    remoteComposerReady: boolean;
    remoteSend: (text: string) => Promise<void>;
    remoteCancel: (queuedItemIDs?: string[]) => Promise<import("../lib/inboxCancel").CancelOutcome>;
    activeSessionIdentity: string;
    sessionSurfaceFence: ReturnType<typeof import("./sessionTarget").createSessionSurfaceFence>;
    sessionOperations: ReturnType<typeof useSessionOperations>;
  };
  surface: Surface;
  stores: {
    composerProfilesByTab: Record<string, ComposerProfile>;
    setComposerProfilesByTab: React.Dispatch<React.SetStateAction<Record<string, ComposerProfile>>>;
    tabMetas: TabMeta[];
    setTabMetas: React.Dispatch<React.SetStateAction<TabMeta[]>>;
    tabOrderIds: string[];
    setTabOrderIds: React.Dispatch<React.SetStateAction<string[]>>;
    yoloRestoreToolApprovalModesRef: { current: Record<string, RestorableToolApprovalMode> };
    userPlanModeByTabRef: { current: UserPlanModeIntents };
  };
  local: {
    setHistView: React.Dispatch<React.SetStateAction<import("./historyViewProjection").HistoryViewState | null>>;
    setTabRevealSignal: React.Dispatch<React.SetStateAction<number>>;
    setTranscriptRevealSignal: React.Dispatch<React.SetStateAction<number>>;
    sidebarImDetailConnectionId: string;
    setSidebarImDetailConnectionId: React.Dispatch<React.SetStateAction<string>>;
    workspaceScopeActiveTabRef: { current: string | undefined };
    workspaceControllerEpoch: number;
    setWorkspaceControllerEpoch: React.Dispatch<React.SetStateAction<number>>;
    dockRefreshKey: number;
    setDockRefreshKey: React.Dispatch<React.SetStateAction<number>>;
    fileRefRefreshKey: number;
    setFileRefRefreshKey: React.Dispatch<React.SetStateAction<number>>;
    projectRevision: number;
    setProjectRevision: React.Dispatch<React.SetStateAction<number>>;
  };
  goal: {
    runGoalAction: ReturnType<typeof import("../lib/goalAction").useGoalActionHandler>["runGoalAction"];
    handleGoalActionError: ReturnType<typeof import("../lib/goalAction").useGoalActionHandler>["handleGoalActionError"];
  };
};

/**
 * Session/composer composition: runs every session-domain owner hook in the
 * App body's original order and returns the bags the navigation composition
 * and the shell view consume. Pure relocation — hook order within the
 * segment is unchanged.
 */
export function useAppSessionComposition(input: AppSessionCompositionInput) {
  const { t, showToast, shell, runtime } = input;
  const {
    state, liveStore, activeTabId, notice, activeTab, remoteSurfaceActive, remoteSession, remoteComposerReady,
    remoteSend, activeSessionIdentity, sessionSurfaceFence, sessionOperations,
  } = input.core;
  // remoteCancel is consumed by the shell view through core.
  const {
    transitioning: runtimeTransitioning, dataReady: navigationTargetDataReady,
    preserved: preservedTranscriptSurface, commitRendered: commitRenderedTranscriptSurface,
    begin: beginNavigationSurface, maskTarget: settleNavigationSurface, commitPaint: commitNavigationSurfacePaint,
  } = input.surface;
  const {
    sendToTab, runShellForTab, steerForTab, cancel, cancelForTab,
    setControllerModeForTab, setCollaborationMode: setControllerCollaborationMode,
    setCollaborationModeForTab: setControllerCollaborationModeForTab,
    setToolApprovalModeForTab, setQualityFloor: setControllerQualityFloor,
    setComposerProfileForTab: setControllerComposerProfileForTab, setGoalForTab: setControllerGoalForTab,
    resumeGoalForTab: resumeControllerGoalForTab, pauseGoalForTab: pauseControllerGoalForTab,
    clearGoalForTab: clearControllerGoalForTab,
    setModelForTab, setEffortForTab,
  } = runtime.composer;
  const {
    recoverDeliveryToTab, approveForTab, isPromptCurrentForTab, resolvePlanDecisionForTab, resolveRecoveryForTab,
    answerQuestionForTab, answerMCPInteractionForTab, dismissExtensionForm, drainExtensionNotifications,
    clearSession, newSession, loadOlderHistory, rewindForTab, rewindForTabDetailed, undoRewindForTab,
    listSessions, openChannelSession, resumeSession,
  } = runtime.sessionActions;
  const {
    switchTab, switchRemoteTab, closeTab, reorderTabs, createIsolatedWorktree,
    noteNavigationIntent, registeredNavigationIntent, isNavigationIntentCurrent, reassertVisibleTabAfterStaleNavigation,
    commitSingleSurfaceNavigation, openTopicSession, openGlobalTab, openProjectTab, activateTopic,
    ensureBlankSurface, ensureBlankTab,
  } = runtime.navigation;
  const {
    setTransientOverlayDismissSignal, managementActive, desktopLayoutStyle,
    singleSurfaceLayout, windowsFramelessChrome, mainWindowMaximised, rightDockMode,
    workspacePanelOpen, workspacePanelMaximized, liveTerminalHeight, setLiveWorkspacePanelRenderWidth,
    setRightDockTreeWidth, terminalPanelOpen, setSettingsTarget, enterConversation,
  } = shell;
  const { sidebarImConnections, reloadConfigWarnings } = shell.preferences;
  const {
    composerProfilesByTab, setComposerProfilesByTab, tabMetas, setTabMetas, tabOrderIds, setTabOrderIds,
    yoloRestoreToolApprovalModesRef, userPlanModeByTabRef,
  } = input.stores;
  const {
    setHistView, setTabRevealSignal, setTranscriptRevealSignal,
    sidebarImDetailConnectionId, setSidebarImDetailConnectionId,
    workspaceScopeActiveTabRef, workspaceControllerEpoch, setWorkspaceControllerEpoch,
    setDockRefreshKey, projectRevision, setProjectRevision,
  } = input.local;
  const { runGoalAction, handleGoalActionError } = input.goal;
  const insertCommands = useComposerInsertCommands({
    activeTabId,
    sessionKey: activeSessionIdentity,
    approval: state.approval,
    operations: sessionOperations,
    t,
    showToast,
    ports: { terminalOutput: (tabId, terminalSessionId) => desktopBridge.terminalOutputForTab(tabId, terminalSessionId) },
  });
  const {
    setInsertTarget: setWorkspaceInsertTarget, replaceComposerInsert,
  } = insertCommands;
  useWindowsMaximisedSync(windowsFramelessChrome);
  useWailsResizeFix(windowsFramelessChrome, mainWindowMaximised);
  const clearCommands = useSessionClearCommands({
    activeTabId,
    activeSessionIdentity,
    remote: remoteSurfaceActive,
    t,
    notice,
    operations: sessionOperations,
    refreshDock: () => setDockRefreshKey((value) => value + 1),
    ports: {
      clearSession,
      clearRemoteSession: (tabId) => desktopBridge.clearRemoteTabSession(tabId),
      retryRemoteHydration: () => remoteSession.retryHydration(),
    },
  });
  const { clearContextPending, setClearContextPending } = clearCommands;
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

  useSidebarConnectionValidity({ connections: sidebarImConnections, setConnectionId: setSidebarImDetailConnectionId });

  useNativeSettingsEvent({ closeTransientOverlays, setSettingsTarget });

  const [footerHeight, setFooterHeight] = useState(0);
  const footerRef = useRef<HTMLElement>(null);
  const commitFooterHeight = useCommittedCommand((height: number) => setFooterHeight(height));
  useFooterHeightLifecycle(footerRef, commitFooterHeight);
  useActiveTabMirrorCommit(activeTabId);
  const { invocationMetadataByTab, handleInvocationMetadataChange } = useInvocationMetadata();
  const shellGeometry = useShellGeometry({ appRef, layoutRef });
  const {
    rightDockTreeWidthClamp, chatReservedWidth,
    workspacePanelAvailableWidth, workspacePanelRenderWidth, workspacePanelOverlay, workspacePanelRenderable,
    workspacePanelGridOpen, sidebarRenderWidth, terminalRenderHeight,
  } = shellGeometry;

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
  const composerSessionKey = useMemo(() => {
    return composerDraftKeyForTab(activeTab, activeTabId);
  }, [activeTab, activeTabId]);
  const transcriptGeometrySessionKey = activeSessionIdentity;
  const workspaceScopeKey = projectWorkspaceScopeKey({
    activeTabId, tabSessionPath: activeTab?.sessionPath, metaSessionPath: state.meta?.sessionPath,
    cwd: state.meta?.cwd, sessionGen: state.sessionGen, workspaceControllerEpoch,
  });
  const workspaceTreeMemoryKey = projectWorkspaceTreeMemoryKey({
    scope: activeTab?.scope, workspaceRoot: activeTab?.workspaceRoot, cwd: state.meta?.cwd,
  });
  const { activeTopicTurns } = useTopicSummary({ activeTab, revision: projectRevision });
  const visibleUserTurns = visibleRuntimeState.items.reduce((count, item) => (item.kind === "user" ? count + 1 : count), 0);
  const currentTabTurns = Math.max(visibleRuntimeState.checkpoints.length, visibleUserTurns);
  const sessionTurns = currentTabTurns > 0 ? currentTabTurns : remoteSurfaceActive ? 0 : activeTopicTurns ?? 0;
  const startupSplashHold = !activeTabId && state.meta?.ready !== true && !state.meta?.startupErr;
  const profileProjection = useComposerProfileProjection({
    activeTabId,
    activeTab,
    meta: state.meta,
    profilesByTab: composerProfilesByTab,
    setProfilesByTab: setComposerProfilesByTab,
    tabMetas,
    remote: remoteSurfaceActive,
    remoteSession,
    planIntentsRef: userPlanModeByTabRef,
    setControllerQualityFloor,
    showToast,
  });
  const {
    composerProfile, goal, collaborationMode, toolApprovalMode,
    patchComposerProfileForTab, patchActivatedGoalForTab,
  } = profileProjection;
  const controllerReady =
    state.meta?.ready === true &&
    (!state.meta.runtime || state.meta.runtime.phase === "ready") &&
    !state.meta.startupErr &&
    !state.backendActivationPending &&
    !runtimeTransitioning;
  useAppDiagnostics({ activeTabId, tabCount: tabMetas.length, ready: controllerReady, running: state.running,
    hydrating: state.hydrating, runtimeTransitioning, contentRevision: state.historyLayoutRevision });

  const tabBarCommands = useTabBarCommands({
    activeTabId,
    tabMetas,
    deliveryWorktreeRoot: state.meta?.workspaceRoot || state.meta?.workspacePath || state.meta?.cwd,
    t,
    showToast,
    setTabMetas,
    setTabOrderIds,
    setComposerProfilesByTab,
    setTabRevealSignal,
    clearWorkspaceConflict: () => setWorkspaceConflict(null),
    ports: {
      closeTab,
      reorderTabs,
      switchTab,
      switchRemoteTab,
      refreshTabMetas: (apply, options) => refreshTabMetas(apply, options),
      refreshBackgroundRuntimes,
      cancelActive: () => void handleCancelActive(),
      noteNavigationIntent,
      beginNavigationSurface,
      settleNavigationSurface,
      isNavigationIntentCurrent,
      reassertVisibleTabAfterStaleNavigation,
      enterChatView: enterConversation,
      createIsolatedWorktree,
    },
  });
  const { pendingClose, setPendingClose } = tabBarCommands;

  const decisionSurface = useMemo((): AppDecisionSurfaceKind | null => projectDecisionSurface({
    approval: state.approval, ask: state.ask, mcpInteraction: state.mcpInteraction, extensionForm: state.extensionForm,
    workspaceConflict, pendingClose, clearContextPending,
  }), [clearContextPending, pendingClose, state.approval, state.ask, state.extensionForm, state.mcpInteraction, workspaceConflict]);
  const visibleDecisionSurface = decisionSurface;
  const composerSurfaceHidden = runtimeTransitioning || Boolean(decisionSurface);
  useDecisionSurfaceFocus({ surface: decisionSurface, activeTabId, closeOverlays: closeTransientOverlays });

  // Extension form surface (stage 8b2): submit delivers the structured values
  // to the owning sidecar; cancel reports values{"cancelled": true} over the
  // same channel. A failed cancel still dismisses — the sidecar that could not
  // be reached is gone either way.
  const extensionSurface = useExtensionSurface({
    activeTabId,
    form: state.extensionForm,
    notifications: state.extensionNotifications,
    dismissForm: dismissExtensionForm,
    drainNotifications: drainExtensionNotifications,
    showToast,
  });
  const extensionStatusList = useMemo(() => Object.values(state.extensionStatuses ?? {}), [state.extensionStatuses]);
  const visibleTabId = activeTabId;
  const visibleTabs = useMemo(() => projectVisibleTabs({
    tabs: tabMetas, orderIds: tabOrderIds, profiles: composerProfilesByTab, visibleTabId, running: state.running,
  }), [composerProfilesByTab, state.running, tabMetas, tabOrderIds, visibleTabId]);

  useTabProjectionLifecycle({
    tabs: tabMetas, activeTabId, activeMeta: activeTab, meta: state.meta,
    yoloRestoreRef: yoloRestoreToolApprovalModesRef, planIntentsRef: userPlanModeByTabRef,
    setOrder: setTabOrderIds, setProfiles: setComposerProfilesByTab,
  });


  const controllerProfiles = projectControllerProfiles(tabMetas, composerProfilesByTab, {
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, profile: composerProfile, remote: remoteSurfaceActive,
  });
  const controllerProfileCommands = useControllerProfileCommands({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, profiles: controllerProfiles,
    ready: controllerReady, remote: remoteSurfaceActive, runtimeEpoch: state.meta?.runtime?.epoch, operations: sessionOperations,
    ports: { model: setModelForTab, profile: setControllerComposerProfileForTab },
    remoteModel: remoteSession.setModel, report: handleGoalActionError,
  });
  const { switchModel, applyProfile: applyControllerProfile } = controllerProfileCommands;
  const hydratePlaceholderActive = Boolean(
    state.hydrating &&
    state.items.length === 0 &&
    state.hydratePlaceholderItems?.length,
  );
  const sessionUndoCommands = useSessionUndo({
    activeTabId,
    activeTabReadOnly: Boolean(activeTab?.readOnly),
    items: state.items,
    hydratePlaceholderActive,
    controllerReady, running: state.running, messageActionOpen: state.messageAction != null,
    approvalOpen: state.approval != null, askOpen: state.ask != null, clearContextPending,
    ports: {
      rewindForTab, rewindForTabDetailed,
      refreshTabMetas: () => void refreshTabMetas(undefined, { afterMutation: true }),
      undoRewindForTab, sendToTab,
      composeInsert: replaceComposerInsert,
      refreshDock: () => setDockRefreshKey((value) => value + 1),
      refreshProject: () => setProjectRevision((value) => value + 1),
    },
  });
  const {
    rewindState, rewindCommitting, rewindSignal, setRewindStateForTab,
    handleSessionRevertCommitted, handleMessageAction, handleUndoRewind, handleEditPrompt,
  } = sessionUndoCommands;
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
  const modeActions = useComposerModeActions({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    remote: remoteSurfaceActive, collaborationMode, toolApprovalMode, goal,
    operations: sessionOperations,
    planIntentsRef: userPlanModeByTabRef,
    yoloRestoreRef: yoloRestoreToolApprovalModesRef,
    ports: {
      setMode: setControllerModeForTab, setCollaboration: setControllerCollaborationModeForTab,
      setApproval: setToolApprovalModeForTab, clearGoal: clearControllerGoalForTab,
      setRemote: setRemoteComposerProfileForSessionAction, drainRemote: drainRemoteApprovalsForTab,
      patch: patchComposerProfileForTab,
    },
    showError: (message) => showToast(message, "error"),
  });
  const { applyCollaborationMode, notePlanModeForTab } = modeActions;
  const rememberPlanRevisionForTab = usePendingPlanRevisions({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity },
    resources: controllerProfiles.map(resource => resource.target), running: state.running,
    ready: controllerReady && !state.approval && !state.ask && !state.mcpInteraction,
    operations: sessionOperations, send: sendRevision, report: reportPendingRevisionFailure,
  });
  const promptCommands = useSessionPromptCommands({
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
  const controlCommands = useSessionControlCommands({
    activeTabId,
    sessionKey: activeSessionIdentity,
    operations: sessionOperations,
    showToast,
    clearWorkspaceConflict: () => setWorkspaceConflict(null),
    ports: {
      cancel,
      cancelForTab,
      acceptDelivery: (tabId) => desktopBridge.acceptDeliveryToTab(tabId),
      disconnectRemote: (hostId) => desktopBridge.disconnectRemoteHost(hostId),
      cancelJobForTab: (tabId, jobId) => desktopBridge.cancelJobForTab(tabId, jobId),
      refreshBackgroundRuntimes,
    },
  });
  const { handleCancelActive } = controlCommands;
  // Shift+Tab toggles only the collaboration axis; Ctrl/Cmd+Y toggles YOLO on the
  // tool-permission axis while preserving the Ask/Auto base mode.
  const cycleMode = useCommittedCommand(() => {
    runGoalAction(() => applyCollaborationMode(collaborationMode === "plan" ? "normal" : "plan"));
  });

  const todoPanelCommands = useTodoPanelCommands({
    items: visibleRuntimeState.items,
    running: visibleRuntimeState.running,
    pendingPrompt: visibleRuntimeState.pendingPrompt,
    meta: state.meta,
    activeTab,
    activeTabId,
    remote: remoteSurfaceActive,
    remoteReady: remoteComposerReady,
    controllerReady,
    sessionKey: activeSessionIdentity,
    operations: sessionOperations,
    t,
    ports: {
      remoteSend: (text) => remoteSend(text),
      sendToTab: (tabId, text) => sendToTab(tabId, text),
      dismissTodoBatch: (tabId, batchKey) => desktopBridge.dismissTodoBatchForTab(tabId, batchKey),
    },
  });
  const { showTodos, scopedTodoBatch, todos, dismissTodos, handleTodoContinue } = todoPanelCommands;

  const sessionTitle = topicTitle(activeTab);
  const exportItems = remoteSurfaceActive ? remoteSession.transcript.items : state.items;
  const exportLive = remoteSurfaceActive
    ? remoteSession.transcript.live
    : liveStore.getSnapshot(activeTabId) ?? state.live;
  const sessionHasContent = exportItems.length > 0 || Boolean(exportLive?.text || exportLive?.reasoning);

  const sessionExportCommands = useSessionExportCommands({
    sessionTitle,
    items: exportItems,
    live: exportLive,
    hasContent: sessionHasContent,
    t,
    showToast,
  });

  useActiveTabUiReset({ activeTabId, setClearPending: setClearContextPending, setInsertTarget: setWorkspaceInsertTarget });

  const routerCommands = useComposerRouter({
    activeTabId,
    goalDraftActive: collaborationMode === "goal" && !goal.trim(),
    t,
    notice,
    showToast,
    ports: {
      runShellForTab,
      switchModel: (name, tabId) => switchModel(name, tabId),
      newSession: () => newSession(),
      setSettingsTarget: (tab) => setSettingsTarget(tab),
      setClearContextPending,
      clearWorkspaceConflict: () => setWorkspaceConflict(null),
      setWorkspaceConflict: (value) => setWorkspaceConflict(value),
      setPendingClose: (value) => setPendingClose(value),
      submitComposerTurn: (tab, display, submit, structured) => submitComposerTurn(tab, display, submit, structured),
      steerForTab,
      isRemoteTab: (tabId) => tabMetas.some((tab) => tab.id === tabId && tab.remote),
    },
  });

  const goalCommands = useComposerGoalCommands({ applyCollaborationMode, applyGoal });
  const remoteGoalActions = useRemoteComposerRuntimeActions({
    target: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, operations: sessionOperations,
    remote: remoteSurfaceActive, session: remoteSession, runGoalAction,
    pauseLocal: pauseControllerGoalForTab, resumeLocal: resumeControllerGoalForTab,
    setLocalEffort: setEffortForTab, showError: (message) => showToast(message, "error"),
  });

  const {
    refreshTabMetas, seedActiveTabMeta,
    handleRuntimeEvent, handleRuntimeReady, handleRuntimeRebuilt,
    handleRemoteStatus, handleRemoteForwards, handleRemoteServer,
    handleInitialRemoteHosts, handleInitialRemoteStatuses,
  } = useRuntimeEventHandlers({
    activeTabId,
    workspaceScopeKey,
    workspaceScopeActiveTabRef,
    userPlanModeByTabRef,
    setTabMetas,
    setTabOrderIds,
    setComposerProfilesByTab,
    setDockRefreshKey,
    setProjectRevision,
    setWorkspaceControllerEpoch,
    setControllerCollaborationMode,
  });

  const refreshProviderSetupState = useCommittedCommand(() => probeProviderSetupState());

  const leaseBlockedTab = activeLeaseBlockedTab(tabMetas, activeTab?.id ?? activeTabId);
  const bannerCommands = useSessionBannerCommands({
    remote: Boolean(activeTab?.remote),
    reloadConfigWarnings,
  });

  const workspacePanelCommands = useWorkspacePanelCommands({
    workspaceRoot: activeTab?.workspaceRoot ?? state.meta?.cwd ?? "",
    creation: desktopLayoutStyle === "creation", visible: surfaceWorkspacePanelRenderable,
    closeOverlays: closeTransientOverlays, clearLiveWidth: setLiveWorkspacePanelRenderWidth,
    availableWidth: workspacePanelAvailableWidth, clampTreeWidth: rightDockTreeWidthClamp, setTreeWidth: setRightDockTreeWidth,
  });
  const { openRightDockMode } = workspacePanelCommands;

  const turnVerificationCommands = useTurnVerificationCommands({
    activeTabId,
    turnStartAt: state.turnStartAt,
    completionSummary: state.completionSummary,
    openChangedDock: () => openRightDockMode("changed"),
  });

  const terminalPanelCommands = useTerminalPanelCommands({
    tabId: activeTabId, enabled: conversationView.localToolsEnabled, shortcutsEnabled: !managementActive,
  });

  const remoteWorkspaceCommands = useRemoteWorkspaceCommands({ t, showToast });

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

  // Coalesce tab-bar switches through the same last-click-wins scheduler that
  // openTopic/blank/resume navigation uses, so rapidly clicking between two
  // running sessions can't run two switchTab() calls concurrently. Concurrent
  // switches race on the backend SetActiveTab/confirmBackendActiveTab ordering,
  const {
    transcriptHydrating, creationEmptyHero,
    visibleTranscriptItems, visibleTranscriptTabId, visibleTranscriptGeometryKey,
    handleLoadOlderHistory, handleSurfacePaintReady, latestGuidanceConsumed, handleTranscriptPrompt,
  } = useTranscriptSurfaceProjection({
    hydrating: state.hydrating,
    hydrateHistoryLoaded: state.hydrateHistoryLoaded,
    hydratePlaceholderItems: state.hydratePlaceholderItems,
    hydratePlaceholderActive,
    items: state.items,
    remote: remoteSurfaceActive,
    remoteItems: remoteSession.transcript.items,
    activeTabId,
    geometrySessionKey: transcriptGeometrySessionKey,
    transitioning: runtimeTransitioning,
    navigationDataReady: navigationTargetDataReady,
    preserved: preservedTranscriptSurface,
    singleSurface: singleSurfaceLayout,
    controllerReady,
    creationLayout: desktopLayoutStyle === "creation",
    imDetailActive: Boolean(sidebarImDetailConnection),
    sessionHasContent,
    commitRendered: commitRenderedTranscriptSurface,
    commitPaint: commitNavigationSurfacePaint,
    commitSingleSurface: commitSingleSurfaceNavigation,
    ports: {
      loadOlderHistory: (tabId, targetTurn, trigger) => loadOlderHistory(tabId, targetTurn, trigger),
      commitThenSend: (tabId, text) => commitThenSend(tabId, text),
    },
  });

  const { handleDeliveryContinue } = useDeliveryContinueCommands({
    surfaceFence: sessionSurfaceFence,
    ready: controllerReady,
    goal: state.meta?.goal,
    t,
    ports: {
      resumeGoal: resumeControllerGoalForTab,
      recoverDelivery: recoverDeliveryToTab,
    },
  });

  const { openAutomationTopic, topicAccepted } = useAutomationNavigation({ noteIntent: noteNavigationIntent,
    enqueue: useCommittedCommand((intent, seq) => enqueueNavigationWithIntent(intent, seq)) });  const { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject } = useDesktopNavigation({
    visible: { tabId: activeTabId ?? "", sessionKey: activeSessionIdentity }, singleSurface: singleSurfaceLayout,
    ports: { isNavigationIntentCurrent, activateTopic, openTopicSession, openGlobalTab, openProjectTab,
      ensureBlankSurface, ensureBlankTab, createIsolatedWorktree, openChannelSession, resumeSession,
      registeredNavigationIntent, switchRemoteTab, openRemoteProject: desktopBridge.openRemoteProjectTab,
      listTabs: desktopBridge.listTabs, applyTabs: setTabMetas, seedTab: seedActiveTabMeta, listSessions, topicAccepted },
    setTabRevealSignal, setTranscriptRevealSignal, setProjectRevision, setHistory: setHistView, t, showToast,
    noteIntent: noteNavigationIntent, beginSurface: beginNavigationSurface, settleSurface: settleNavigationSurface,
    showChat: enterConversation,
  });
  return {
    insertCommands,
    clearCommands,
    tabBarCommands,
    extensionSurface,
    promptCommands,
    controlCommands,
    routerCommands,
    goalCommands,
    remoteGoalActions,
    modeActions,
    controllerProfileCommands,
    profileProjection,
    sessionExportCommands,
    workspacePanelCommands,
    turnVerificationCommands,
    terminalPanelCommands,
    remoteWorkspaceCommands,
    bannerCommands,
    runtimeEventCommands: {
      refreshTabMetas, seedActiveTabMeta,
      handleRuntimeEvent, handleRuntimeReady, handleRuntimeRebuilt,
      handleRemoteStatus, handleRemoteForwards, handleRemoteServer,
      handleInitialRemoteHosts, handleInitialRemoteStatuses,
    },
    sessionUndo: {
      rewindState, rewindCommitting, rewindSignal, handleSessionRevertCommitted, handleMessageAction, handleUndoRewind, handleEditPrompt,
    },
    todoPanel: { showTodos, scopedTodoBatch, todos, dismissTodos, handleTodoContinue },
    delivery: { handleDeliveryContinue },
    transcript: {
      transcriptHydrating, creationEmptyHero,
      visibleTranscriptItems, visibleTranscriptTabId, visibleTranscriptGeometryKey,
      handleLoadOlderHistory, handleSurfacePaintReady, latestGuidanceConsumed, handleTranscriptPrompt,
    },
    automation: { openAutomationTopic },
    desktopNavigation: { enqueueNavigation, enqueueNavigationWithIntent, openRemoteProject },
    invocation: { invocationMetadataByTab, handleInvocationMetadataChange },
    sessionHasContent,
    conversationView,
    visibleRuntimeState,
    sidebarImDetailConnection,
    surfaceWorkspacePanelRenderable,
    surfaceWorkspacePanelGridOpen,
    surfaceWorkspacePanelOverlay,
    terminalSurfaceOpen,
    statusBarVisible,
    chatSurfaceVisible,
    composerSessionKey,
    workspaceScopeKey,
    workspaceTreeMemoryKey,
    sessionTurns,
    startupSplashHold,
    controllerReady,
    decisionSurface,
    visibleDecisionSurface,
    composerSurfaceHidden,
    extensionStatusList,
    visibleTabs,
    visibleTabId,
    hydratePlaceholderActive,
    leaseBlockedTab,
    layoutStyle,
    cycleMode,
    remoteComposerSend,
    closeTransientOverlays,
    refreshProviderSetupState,
    shellGeometry,
    appRef,
    layoutRef,
    footerHeight,
    footerRef,
    backgroundRuntimes,
    workspaceConflict,
  };
}
