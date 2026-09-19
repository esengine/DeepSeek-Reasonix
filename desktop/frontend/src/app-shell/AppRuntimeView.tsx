import { lazy, Suspense, useMemo, useState, useLayoutEffect, type CSSProperties } from "react";
import { DockNavigation } from "./dockNavigation";
import { useFileNavigationRuntime } from "../app-runtime/useFileNavigationRuntime";
import { fileNavigationKey } from "../lib/fileNavigationOwner";
import { useActivityBarStore } from "../store/activityBar";
import { ShellExpandProvider } from "../lib/shellExpand";
import { RemoteNavigationContext } from "../lib/remoteNavigationCommands";
import { UpdaterProvider } from "../lib/useUpdater";
import type { State } from "../lib/useController";
import type { TabMeta } from "../lib/types";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import type { Translator } from "../lib/i18n";
import type { useAppRuntimeAdapter } from "../app-runtime/useAppRuntimeAdapter";
import type { useNavigationSurface } from "../lib/useNavigationSurface";
import type { useAppShellStores } from "../app-runtime/useAppShellStores";
import type { useAppSessionComposition } from "../app-runtime/useAppSessionComposition";
import type { useAppNavigationComposition } from "../app-runtime/useAppNavigationComposition";
import { draftSurfaceNeedsAttention, type useSessionDraftSurface } from "../app-runtime/useSessionDraftSurface";
import type { HistoryViewState } from "../app-runtime/historyViewProjection";
import { ShellHotkeys, TextSizeHotkeys } from "./HotkeyRegistrations";
import { WindowChromeLifecycle } from "../app-runtime/WindowChromeLifecycle";
import { StartupGateLifecycle } from "../app-runtime/StartupGateLifecycle";
import { AppRuntimeEffects } from "../app-runtime/AppRuntimeEffects";
import { ThemeBackground } from "../components/ThemeBackground";
import { useTopicbarHeightVar } from "../lib/useTopicbarHeightVar";
import { SidebarRegion } from "./SidebarRegion";
import { TopicbarRegion } from "./TopicbarRegion";
import { buildTopicbarView, TopicbarActionsStack } from "./TopicbarActionsStack";
import { DraftTopicbarActions } from "./DraftTopicbarActions";
import { DockToggleButton } from "./DockToggleButton";
import { LauncherToggleButton } from "./LauncherToggleButton";
import { SessionStatusBanners } from "./SessionStatusBanners";
import { ChatPaneRegion } from "./ChatPaneRegion";
import { DecisionFooterRegion } from "./DecisionFooterRegion";
import { WorkspaceDockRegion } from "./WorkspaceDockRegion";
import { AppBottomRegions } from "./AppBottomRegions";
import { AppOverlayHost } from "./AppOverlayHost";
import { buildAppShellClassNames, buildSessionStatusBannerProps, buildSidebarRegionProps } from "./chromeRegionBuilders";
import { buildBottomRegionsProps, buildWorkspaceDockProps } from "./dockRegionBuilders";
import { buildOverlayHostProps } from "./overlayBuilders";
import { buildComposerSurface, buildDecisionFooterSurface, buildFooterTodo, buildFooterUndo } from "./decisionFooterBuilders";

const WindowsWindowControls = lazy(() => import("./WindowsWindowControls").then((module) => ({ default: module.WindowsWindowControls })));
const DockLauncher = lazy(() => import("../components/DockLauncher").then((module) => ({ default: module.DockLauncher })));

const WORKSPACE_RESIZER_WIDTH = 8;
const SHOW_CONTEXT_DOCK = true;

type Runtime = ReturnType<typeof useAppRuntimeAdapter>;
type Shell = ReturnType<typeof useAppShellStores>;
type SessionComposition = ReturnType<typeof useAppSessionComposition>;
type NavigationComposition = ReturnType<typeof useAppNavigationComposition>;
type LiveStore = Runtime["snapshot"]["liveStore"];

export type AppRuntimeViewProps = {
  core: {
    state: State;
    activeTab: TabMeta | undefined;
    activeTabId: string | undefined;
    liveStore: LiveStore;
    remoteSurfaceActive: boolean;
    remoteSession: RemoteSessionApi;
    remoteComposerReady: boolean;
    remoteCancel: (queuedItemIDs?: string[]) => Promise<import("../lib/inboxCancel").CancelOutcome>;
    surface: ReturnType<typeof useNavigationSurface>;
    t: Translator;
    locale: string;
    onOpenLink: (url: string) => void;
  };
  shell: Shell;
  session: SessionComposition;
  navigation: NavigationComposition;
  runtime: Runtime;
  draft: ReturnType<typeof useSessionDraftSurface>;
  local: {
    tasksOpen: false | "session" | "all";
    setTasksOpen: React.Dispatch<React.SetStateAction<false | "session" | "all">>;
    sidebarImDetailConnectionId: string;
    setSidebarImDetailConnectionId: React.Dispatch<React.SetStateAction<string>>;
    tabRevealSignal: number;
    histView: HistoryViewState | null;
    projectRevision: number;
    dockRefreshKey: number;
    composerFileRefRefreshKey: string;
    refreshComposerFileRefs: () => void;
    terminalContentVisible: boolean;
    terminalFitEnabled: boolean;
    prefetchTerminalPanel: () => void;
  };
};

/**
 * Pure assembly of the App shell tree: every region receives its props from
 * the session/navigation composition bags and the caller's stores. No hooks
 * beyond value memoization live here; ownership stays in the compositions.
 */
export function AppRuntimeView(props: AppRuntimeViewProps) {
  const [dockNavigation] = useState(() => new DockNavigation());
  const fileNavigation = useFileNavigationRuntime();
  useLayoutEffect(() => dockNavigation.attach(), [dockNavigation]);
  useLayoutEffect(() => useActivityBarStore.subscribe(state => dockNavigation.reconcile(state.tabs.map(tab => tab.id))), [dockNavigation]);
  // The open dock tabs are the set of live records: closing a tab, or switching
  // to a project whose tab list has none of them, ends those records. A session
  // change inside one project keeps the dock and only rebinds its credentials.
  useLayoutEffect(() => {
    const retain = () => fileNavigation.retain(
      useActivityBarStore.getState().tabs.map(tab => fileNavigationKey({ sessionTabId: "", dockTabId: tab.id })),
    );
    retain();
    return useActivityBarStore.subscribe(retain);
  }, [fileNavigation]);
  useTopicbarHeightVar();
  const { core, shell, session, navigation, runtime, local, draft } = props;
  const { state, activeTab, activeTabId, t, locale } = core;
  const { windowsFramelessChrome, mainWindowMaximised } = shell;
  const draftActive = Boolean(draft.surface);
  const draftNeedsAttention = draft.surface ? draftSurfaceNeedsAttention(draft.surface) : false;
  const {
    conversationView, visibleRuntimeState, sidebarImDetailConnection,
    surfaceWorkspacePanelRenderable, surfaceWorkspacePanelGridOpen, surfaceWorkspacePanelOverlay, terminalSurfaceOpen,
    controllerReady, decisionSurface, visibleDecisionSurface, composerSurfaceHidden,
    shellGeometry, appRef, layoutRef, footerHeight, footerRef,
  } = session;
  const { chromeCommands, navigationCommands } = navigation;
  const runtimeTransitioning = core.surface.transitioning;
  const presentationTransitioning = runtimeTransitioning && core.remoteSurfaceActive;
  const browserPreviewChrome = navigation.browserPreviewChrome;
  // A draft owns its workspace independently from whichever formal tab was
  // visible before it opened. Build the composer context from that owner so a
  // restored draft never shows the previous project's name or loses the bar.
  const draftWorkspace = draft.surface?.draft.scope === "project"
    ? (draft.surface.draft.workspaceRoot ?? "").trim()
    : "";
  const workspaceContextProject = Boolean(
    activeTab?.remote || (activeTab?.scope === "project" && activeTab.workspaceRoot) || draftWorkspace,
  );
  const workspaceContextRoot = workspaceContextProject
    ? draftWorkspace || (activeTab?.workspaceRoot ?? state.meta?.workspaceRoot ?? state.meta?.cwd ?? "")
    : "";

  const workbenchChromeHidden = true;
  const sidebarClassName = [
    "sidebar",
    shell.sidebarCollapsed ? "sidebar--collapsed" : "",
    "sidebar--workbench",
  ].filter(Boolean).join(" ");
  const startupSplashHold = !activeTabId && state.meta?.ready !== true && !state.meta?.startupErr;

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${shellGeometry.sidebarRenderWidth}px`,
        "--chat-min-width": `${shellGeometry.chatReservedWidth}px`,
        "--workspace-width": `${shellGeometry.workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
        "--terminal-height": `${!draftActive && terminalSurfaceOpen ? shell.liveTerminalHeight ?? shellGeometry.terminalRenderHeight : 0}px`,
      }) as CSSProperties,
    [draftActive, shellGeometry.chatReservedWidth, shell.liveTerminalHeight, shellGeometry.sidebarRenderWidth, shellGeometry.terminalRenderHeight, terminalSurfaceOpen, shellGeometry.workspacePanelRenderWidth],
  );

  const shellClassNames = buildAppShellClassNames({
    platform: shell.desktopPlatform,
    windowsFrameless: windowsFramelessChrome,
    browserPreview: browserPreviewChrome,
    imDetailActive: Boolean(sidebarImDetailConnection),
    sidebarCollapsed: shell.sidebarCollapsed,
    sidebarResizing: shell.sidebarResizing,
    dockGridOpen: !draftActive && surfaceWorkspacePanelGridOpen,
    dockOverlay: !draftActive && surfaceWorkspacePanelOverlay,
    terminalOpen: !draftActive && terminalSurfaceOpen,
    terminalResizing: shell.terminalResizing,
    dockOpen: !draftActive && shell.workspacePanelOpen,
    dockMaximized: shell.workspacePanelMaximized,
    dockResizing: shell.workspacePanelResizing,
  });
  const footerTodo = buildFooterTodo({
    show: session.todoPanel.showTodos,
    identity: session.todoPanel.scopedTodoBatch,
    todos: session.todoPanel.todos,
    running: visibleRuntimeState.running,
    pendingPrompt: visibleRuntimeState.pendingPrompt,
    continueReady: Boolean(activeTabId && !activeTab?.readOnly && (core.remoteSurfaceActive ? core.remoteComposerReady : controllerReady)),
    onContinue: session.todoPanel.handleTodoContinue,
    onDismiss: session.todoPanel.dismissTodos,
  });
  const footerUndo = buildFooterUndo({ rewindState: session.sessionUndo.rewindState, activeTabId, onUndo: session.sessionUndo.handleUndoRewind });
  const decisionFooterSurface = buildDecisionFooterSurface({
    view: {
      surface: visibleDecisionSurface,
      activeTabId,
      cwd: state.meta?.cwd,
      workspaceScopeKey: session.workspaceScopeKey,
      approval: state.approval,
      ask: state.ask,
      mcpInteraction: state.mcpInteraction,
      extensionForm: state.extensionForm,
      workspaceConflict: session.workspaceConflict,
      toolApprovalMode: session.profileProjection.toolApprovalMode,
      insertRequest: session.insertCommands.activePlanRevisionInsertRequest,
    },
    prompts: session.promptCommands,
    extension: session.extensionSurface,
    tabs: session.tabBarCommands,
    clear: session.clearCommands,
    onStop: () => void session.controlCommands.handleCancelActive(),
    cancelWorkspaceConflict: session.controlCommands.cancelWorkspaceConflict,
    onOpenLink: core.onOpenLink,
    onRevisionActiveChange: session.insertCommands.handleRevisionActiveChange,
    t,
  });

  return (
    <ShellExpandProvider>
    <RemoteNavigationContext.Provider value={session.desktopNavigation.openRemoteProject}>
    <UpdaterProvider>
    <ShellHotkeys />
    <TextSizeHotkeys />
    <WindowChromeLifecycle />
    <StartupGateLifecycle />
    <AppRuntimeEffects
      running={state.running}
      onEvent={session.runtimeEventCommands.handleRuntimeEvent}
      onReady={session.runtimeEventCommands.handleRuntimeReady}
      onRebuilt={session.runtimeEventCommands.handleRuntimeRebuilt}
      onRemoteStatus={session.runtimeEventCommands.handleRemoteStatus}
      onRemoteForwards={session.runtimeEventCommands.handleRemoteForwards}
      onRemoteServer={session.runtimeEventCommands.handleRemoteServer}
      onInitialRemoteHosts={session.runtimeEventCommands.handleInitialRemoteHosts}
      onInitialRemoteStatuses={session.runtimeEventCommands.handleInitialRemoteStatuses}
    />
      <div
        ref={appRef}
        onDoubleClickCapture={chromeCommands.handleChromeTitlebarDoubleClick}
        className={shellClassNames.app}
    >
      <ThemeBackground />
      <div
        ref={layoutRef}
        className={shellClassNames.layout}
        style={layoutStyle}
      >
        <a className="skip-to-composer" href="#composer-input">
          {t("shortcuts.skipToComposer")}
        </a>

        <SidebarRegion {...buildSidebarRegionProps({
          className: sidebarClassName,
          shell,
          t,
          geometry: shellGeometry,
          projectTree: {
            activeTab: draftActive ? undefined : activeTab,
            activeScope: draft.surface?.draft.scope,
            activeWorkspaceRoot: draft.surface?.draft.workspaceRoot,
            imTopicSources: shell.preferences.imTopicSources, refreshSignal: local.projectRevision,
            searchExpanded: true, searchFocusSignal: shell.sidebarSearchFocusSignal,
            showShortcutBadges: navigation.topicShortcuts.showTopicBadges, shortcutPlatform: shell.desktopPlatform,
            onVisibleTopicsChange: navigation.topicShortcuts.handleVisibleTopicsChange,
            draftSummaries: draft.summaries,
            onOpenDraft: draft.open,
          },
          topics: navigation.projectTopicCommands,
          paletteShortcut: navigation.commandPaletteShortcut,
          commands: {
            onNewSession: () => void navigationCommands.handleNewTab(),
            onOpenPalette: () => void navigation.paletteCommands.openPalette(),
            onOpenTrash: () => void navigation.historyCommands.openTrash(),
            onOpenAutomation: () => shell.openPage({ kind: "automation" }),
            onOpenSettings: chromeCommands.openSidebarSettings,
            onOpenTopic: navigationCommands.handleOpenTopic,
          },
        })} />

        <TopicbarRegion view={buildTopicbarView({
            t, locale, activeTab: draftActive ? undefined : activeTab, cwd: draft.surface?.draft.workspaceRoot ?? state.meta?.cwd,
            imDetail: draftActive ? null : sidebarImDetailConnection, imTopicSources: shell.preferences.imTopicSources,
            chromeHidden: workbenchChromeHidden, windowsBrand: windowsFramelessChrome,
            automationReturn: shell.automationReturn,
            sidebar: { title: navigation.sidebarToggleTitle, blocked: navigation.sidebarExpandBlocked, pressed: shell.sidebarTogglePressed, collapsed: shell.sidebarCollapsed },
            rename: { editing: !draftActive && navigation.projectTopicCommands.topicbarEditing, draft: navigation.projectTopicCommands.topicTitleDraft },
            draft: draft.surface ? {
              title: t("draft.badge"),
              workspaceLabel: draft.surface.draft.scope === "project"
                ? draft.surface.draft.workspaceRoot.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || draft.surface.draft.workspaceRoot
                : t("draft.globalWorkspace"),
            } : undefined,
          })} commands={{
            openAutomation: () => shell.openPage({ kind: "automation" }), toggleSidebar: shellGeometry.toggleSidebar,
            setTitleDraft: navigation.projectTopicCommands.setTopicTitleDraft, commitRename: navigation.projectTopicCommands.commitActiveTopicRename, cancelRename: navigation.projectTopicCommands.cancelActiveTopicRename,
            startRename: navigation.projectTopicCommands.startActiveTopicRename, openWorktree: navigation.worktreeMergeCommands.openWorktreeMerge,
          }}>
            {draft.surface ? <DraftTopicbarActions
              t={t}
              draft={draft.surface}
              onSetMCPEnabled={draft.setMCPEnabled}
              onDiscard={() => void draft.confirmDiscard({
                title: t("draft.discardTitle"),
                message: t("draft.discardTitle"),
                detail: t("draft.discardDetail"),
                confirmLabel: t("draft.discardConfirm"),
                cancelLabel: t("common.cancel"),
              })}
            /> : <TopicbarActionsStack
              t={t}
              activeTab={activeTab}
              activeTabId={activeTabId}
              imDetailActive={Boolean(sidebarImDetailConnection)}
              dismissSignal={shell.transientOverlayDismissSignal}
              sessionHasContent={session.sessionHasContent}
              exportCommands={session.sessionExportCommands}
              terminal={{ toggle: session.terminalPanelCommands.toggleTerminalPanel, enabled: !core.remoteSurfaceActive, open: shell.terminalPanelOpen && !core.remoteSurfaceActive, prefetch: local.prefetchTerminalPanel }}
              tasksOpen={local.tasksOpen}
              setTasksOpen={local.setTasksOpen}
              onCloseTasks={() => local.setTasksOpen(false)}
              onOpenTaskSession={navigationCommands.openTaskMonitorSession}
              dockToggle={<DockToggleButton renderable={surfaceWorkspacePanelRenderable} t={t} onToggle={session.workspacePanelCommands.toggleWorkspacePanel} />}
              launcherToggle={<LauncherToggleButton
                visible={session.workspacePanelCommands.launcherCard.visible}
                t={t}
                onToggle={session.workspacePanelCommands.toggleLauncherCard}
              />}
            />}
          </TopicbarRegion>

        <section className={`chat-pane${session.transcript.emptyHero || (draftActive && !draftNeedsAttention) ? " chat-pane--creation-empty" : ""}`}>
          {!draft.surface && <SessionStatusBanners {...buildSessionStatusBannerProps({
            t,
            activeTab,
            leaseBlocked: session.leaseBlockedTab ? { tabId: session.leaseBlockedTab.id, message: session.leaseBlockedTab.runtime!.issue!.message } : null,
            meta: state.meta,
            configWarnings: shell.preferences.configLoadWarnings,
            dismissConfigWarnings: shell.preferences.dismissConfigWarnings,
            updateChecksEnabled: shell.preferences.startupUpdateChecksEnabled === true,
            shell,
            banners: session.bannerCommands,
            onboarding: navigation.onboardingCommands,
          })} />}

          <ChatPaneRegion
            // Local navigation is now history-first: keep the transcript
            // mounted while the controller is rebuilt in the background.  The
            // transcript's own hydrating/readiness state still reports progress
            // and the controllerReady fence keeps writes disabled.  Remote
            // surfaces retain their existing transition gate because they do
            // not share the local runtime/cache ownership model.
            transitioning={presentationTransitioning}
            t={t}
            imDetail={sidebarImDetailConnection ? {
              connection: sidebarImDetailConnection,
              onClose: () => local.setSidebarImDetailConnectionId(""),
              onOpenSettings: chromeCommands.openBotSettings,
              onManageAllowlist: chromeCommands.openBotAllowlistSettings,
              onOpenSession: (connection) => void navigationCommands.openSidebarImConnectionSession(connection),
            } : null}
            remote={activeTab?.remote ? { tab: activeTab, session: core.remoteSession } : undefined}
            draft={draft.surface ? {
              surface: draft.surface,
              onUseSaved: draft.useSavedConflict,
              onKeepLocal: () => void draft.keepLocalConflict(),
              onRetrySave: () => void draft.retrySave(),
              onDismissTaskError: () => draft.reportTaskError(draft.surface!.draft.id, draft.surface!.generation, ""),
              onResume: () => void draft.resumeSubmission(),
              onOpenSession: () => void draft.openAcceptedSession(),
              onCheckSubmission: () => void draft.refreshSubmission(),
            } : undefined}
            launcher={!draftActive && session.workspacePanelCommands.launcherCardMounted && !core.remoteSurfaceActive ? (
              <Suspense fallback={null}>
                <DockLauncher
                  tabId={activeTabId ?? ""}
                  scopeKey={session.workspaceScopeKey}
                  workspaceRoot={activeTab?.workspaceRoot ?? state.meta?.cwd ?? ""}
                  visible={!shell.managementActive && !sidebarImDetailConnection}
                  onSelect={session.workspacePanelCommands.openDockEntry}
                  gitBranch={state.meta?.gitBranch}
                  onSpaceModeChange={session.workspacePanelCommands.setLauncherSpaceMode}
                  overlay={session.workspacePanelCommands.launcherCardOverlay}
                />
              </Suspense>
            ) : null}
            transcript={{
              state,
              items: session.transcript.visibleTranscriptItems,
              tabId: session.transcript.visibleTranscriptTabId,
              geometrySessionKey: session.transcript.visibleTranscriptGeometryKey,
              footerHeight,
              invocationMetadata: session.transcript.visibleTranscriptTabId ? session.invocation.invocationMetadataByTab[session.transcript.visibleTranscriptTabId] : undefined,
              surfaceCommitToken: core.surface.surfaceCommitToken,
              liveStore: core.liveStore,
              transcriptHydrating: session.transcript.transcriptHydrating,
              navigationDataReady: core.surface.dataReady,
              readOnly: Boolean(activeTab?.readOnly),
              controllerReady,
              hydratePlaceholderActive: session.hydratePlaceholderActive,
              clearContextPending: session.clearCommands.clearContextPending,
              emptyHero: session.transcript.emptyHero,
              availability: session.transcript.availability,
              rewind: { stateActive: session.sessionUndo.rewindState != null, committing: session.sessionUndo.rewindCommitting },
            }}
            onRetryHistory={() => runtime.sessionActions.retrySessionHistory(activeTabId)}
            commands={{
              onPrompt: session.transcript.handleTranscriptPrompt,
              onFork: (turnId) => session.sessionUndo.handleForkTurn(turnId),
              onLoadOlderHistory: session.transcript.handleLoadOlderHistory,
              onLoadNewerHistory: session.transcript.handleLoadNewerHistory,
              onSurfacePaintReady: session.transcript.handleSurfacePaintReady,
            }}
          />
          <DecisionFooterRegion
            hidden={Boolean(sidebarImDetailConnection)}
            className={["footer", terminalSurfaceOpen ? "footer--compact" : "", visibleDecisionSurface ? "footer--decision" : "", presentationTransitioning ? "footer--navigation-hidden" : ""].filter(Boolean).join(" ")}
            footerRef={footerRef}
            style={core.surface.surface?.phase === "source-retained" && footerHeight > 0 ? { height: footerHeight, minHeight: footerHeight, boxSizing: "border-box" } : undefined}
            todo={draft.surface ? undefined : footerTodo}
            undo={draft.surface ? undefined : footerUndo}
            decision={draft.surface ? undefined : decisionFooterSurface}
            composer={buildComposerSurface({
              view: {
                hidden: composerSurfaceHidden,
                inert: presentationTransitioning,
                hero: session.transcript.emptyHero,
                headline: t("welcome.creation.title"),
                remote: core.remoteSurfaceActive,
                rewindCommitting: session.sessionUndo.rewindCommitting,
                messageActionPending: state.messageAction != null,
                decisionActive: Boolean(decisionSurface),
                runtimeTransitioning: presentationTransitioning,
                controllerReady: controllerReady && session.transcript.availability.kind === "ready",
                submitDisabledReason: session.transcript.availability.kind !== "ready" && session.transcript.availability.source !== "runtime"
                  ? t("sessionRecovery.sendAfterRecovery") : undefined,
                showContextWindowRing: false,
                draftHint: t("draft.createOnSend"),
              },
              base: conversationView.composer,
              tab: activeTab,
              tabId: activeTabId,
              profile: session.profileProjection,
              router: session.routerCommands,
              modes: session.modeActions,
              goals: session.goalCommands,
              remoteGoal: session.remoteGoalActions,
              modelSwitch: session.controllerProfileCommands,
              inserts: session.insertCommands,
              control: session.controlCommands,
              remoteComposer: {
                send: session.remoteComposerSend,
                cancel: core.remoteCancel,
                ready: core.remoteComposerReady,
                profileReady: session.profileProjection.remoteComposerProfileReady,
                liveStore: core.remoteSession.liveStore,
              },
              localLiveStore: core.liveStore,
              onInvocationMetadataChange: session.invocation.handleInvocationMetadataChange,
              onCycleMode: session.cycleMode,
              transientDismissSignal: shell.transientOverlayDismissSignal,
              sessionKey: session.composerSessionKey,
              workspaceScopeKey: session.workspaceScopeKey,
              workspaceContext: {
                scope: workspaceContextProject ? "project" : "global",
                workspaceRoot: workspaceContextRoot,
                workspaceName: workspaceContextProject
                  ? draftWorkspace
                    ? workspaceContextRoot.replace(/[\\/]+$/, "").split(/[\\/]/).filter(Boolean).pop()
                    : activeTab?.workspaceName ?? state.meta?.workspaceName
                  : undefined,
                gitBranch: workspaceContextProject && !activeTab?.remote ? state.meta?.gitBranch : undefined,
                tabId: activeTabId,
                scopeKey: session.workspaceScopeKey,
                remote: Boolean(activeTab?.remote),
                onSwitchWorkspace: navigation.projectTopicCommands.onAddProject,
                onWorkWithoutProject: () => navigationCommands.openBlankSession("global", ""),
                onRefreshProjects: navigation.projectTopicCommands.refreshProjectsAndTabs,
              },
              fileRefRefreshKey: local.composerFileRefRefreshKey,
              guidance: session.transcript.latestGuidanceConsumed,
              guidanceQueuePreviewItems: navigation.guidanceQueueMockItems,
              draft,
            })}
          />
        </section>

        <WorkspaceDockRegion navigation={dockNavigation} fileNavigation={fileNavigation} workspaceRoot={activeTab?.workspaceRoot ?? state.meta?.cwd ?? ""} {...buildWorkspaceDockProps({
          surface: { renderable: !draftActive && surfaceWorkspacePanelRenderable, overlay: !draftActive && surfaceWorkspacePanelOverlay, gridOpen: !draftActive && surfaceWorkspacePanelGridOpen },
          showContext: SHOW_CONTEXT_DOCK,
          remote: core.remoteSurfaceActive,
          t,
          context: conversationView.context,
          sessionTurns: session.sessionTurns,
          contextRefreshKey: local.dockRefreshKey + visibleRuntimeState.contextPanelSeq,
          workspaceKey: session.workspaceTreeMemoryKey,
          workspaceScopeKey: session.workspaceScopeKey,
          mode: shell.rightDockMode,
          meta: state.meta,
          tabId: activeTabId,
          completionSummary: state.completionSummary,
          turnStartAt: state.turnStartAt,
          layout: { treeWidth: shell.rightDockTreeWidth, previewWidth: shell.rightDockPreviewWidth, maximized: shell.workspacePanelMaximized },
          geometry: shellGeometry,
          panels: session.workspacePanelCommands,
          inserts: session.insertCommands,
          verification: session.turnVerificationCommands,
          onFileTreeRefresh: local.refreshComposerFileRefs,
          onSessionRevertCommitted: session.sessionUndo.handleSessionRevertCommitted,
          onOpenInTerminal: core.remoteSurfaceActive ? undefined : session.terminalPanelCommands.openTerminalForPath,
        })} />
        <AppBottomRegions {...buildBottomRegionsProps({
          t,
          chatSurfaceVisible: session.chatSurfaceVisible,
          surfaceOpen: !draftActive && terminalSurfaceOpen,
          contentVisible: local.terminalContentVisible,
          remote: core.remoteSurfaceActive,
          readOnly: Boolean(activeTab?.readOnly),
          tabId: activeTabId,
          meta: state.meta,
          fitEnabled: local.terminalFitEnabled,
          liveTerminalHeight: shell.liveTerminalHeight,
          geometry: shellGeometry,
          terminal: {
            onClose: session.terminalPanelCommands.closeTerminalPanel,
            onAddOutput: (sessionId) => void session.insertCommands.addTerminalOutputToComposer(sessionId),
            onAddToChat: session.insertCommands.addTerminalSelectionToComposer,
          },
          status: draftActive || !session.statusBarVisible ? undefined : {
            base: conversationView.status,
            rewindCommitting: session.sessionUndo.rewindCommitting,
            sessionTurns: session.sessionTurns,
            labelStyle: shell.preferences.statusBarStyle,
            items: shell.preferences.statusBarItems,
            extensionStatuses: session.extensionStatusList,
            remoteHosts: shell.remoteHosts,
            remoteStatuses: shell.remoteStatuses,
            onCancelJob: core.remoteSurfaceActive ? core.remoteSession.cancelJob : runtime.composer.cancelJob,
            onCancelRuntimeJob: session.controlCommands.cancelRuntimeJob,
            onRevealRuntime: session.tabBarCommands.revealBackgroundRuntime,
            onConnectRemote: session.remoteWorkspaceCommands.connectAndOpenRemoteWorkspace,
            onDisconnectRemote: session.controlCommands.handleDisconnectRemote,
            onManageRemote: () => shell.setSettingsTarget("remote"),
            onOpenRemote: shell.requestRemoteExplorer,
            onOpenRemoteWorkspace: session.remoteWorkspaceCommands.openRemoteWorkspaceFromStatus,
          },
        })} />
      </div>

      <AppOverlayHost {...buildOverlayHostProps({
        t,
        running: state.running,
        histView: local.histView,
        pageKind: shell.page.kind,
        activeTab,
        activeTabId,
        cwd: state.meta?.cwd,
        paletteItems: navigation.paletteCommands.paletteItems,
        startupSplashHold,
        automationTopic: session.automation.openAutomationTopic,
        shell,
        history: navigation.historyCommands,
        navigation: navigationCommands,
        chrome: chromeCommands,
        onboarding: navigation.onboardingCommands,
        worktree: navigation.worktreeMergeCommands,
        prefillSubagentCommand: session.insertCommands.prefillSubagentCommand,
        sessionActions: {
          previewSession: runtime.sessionActions.previewSession,
          listTrashedSessions: runtime.sessionActions.listTrashedSessions,
          restoreSession: runtime.sessionActions.restoreSession,
          purgeTrashedSession: runtime.sessionActions.purgeTrashedSession,
        },
        setSettingsTarget: shell.setSettingsTarget,
      })} />
      {windowsFramelessChrome && (
        <WindowsWindowControls
          maximised={mainWindowMaximised}
          onMinimize={chromeCommands.minimizeMainWindow}
          onToggleMaximize={chromeCommands.toggleMainWindowMaximized}
          onClose={chromeCommands.closeMainWindow}
        />
      )}
    </div>
    </UpdaterProvider>
    </RemoteNavigationContext.Provider>
    </ShellExpandProvider>
  );
}
