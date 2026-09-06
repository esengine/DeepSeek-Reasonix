import { lazy, useMemo, type CSSProperties } from "react";
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
import type { HistoryViewState } from "../app-runtime/historyViewProjection";
import type { TopicTimeFilter } from "../app-runtime/useLocalUiLifecycles";
import { ShellHotkeys, TextSizeHotkeys } from "./HotkeyRegistrations";
import { WindowChromeLifecycle } from "../app-runtime/WindowChromeLifecycle";
import { StartupGateLifecycle } from "../app-runtime/StartupGateLifecycle";
import { AppRuntimeEffects } from "../app-runtime/AppRuntimeEffects";
import { ThemeBackground } from "../components/ThemeBackground";
import { AppChrome } from "../components/AppChrome";
import { SidebarRegion } from "./SidebarRegion";
import { TopicbarRegion } from "./TopicbarRegion";
import { buildTopicbarView, TopicbarActionsStack } from "./TopicbarActionsStack";
import { DockToggleButton } from "./DockToggleButton";
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
  local: {
    tasksOpen: false | "session" | "all";
    setTasksOpen: React.Dispatch<React.SetStateAction<false | "session" | "all">>;
    topicTimeFilter: TopicTimeFilter;
    setTopicTimeFilter: (value: TopicTimeFilter) => void;
    sidebarImDetailConnectionId: string;
    setSidebarImDetailConnectionId: React.Dispatch<React.SetStateAction<string>>;
    tabRevealSignal: number;
    transcriptRevealSignal: number;
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
  const { core, shell, session, navigation, runtime, local } = props;
  const { state, activeTab, activeTabId, t, locale } = core;
  const { sidebarWorkbench, sidebarCreation, windowsFramelessChrome, managementActive, mainWindowMaximised } = shell;
  const {
    conversationView, visibleRuntimeState, sidebarImDetailConnection,
    surfaceWorkspacePanelRenderable, surfaceWorkspacePanelGridOpen, surfaceWorkspacePanelOverlay, terminalSurfaceOpen,
    controllerReady, decisionSurface, visibleDecisionSurface, composerSurfaceHidden,
    shellGeometry, appRef, layoutRef, footerHeight, footerRef,
  } = session;
  const { chromeCommands, navigationCommands } = navigation;
  const runtimeTransitioning = core.surface.transitioning;
  const browserPreviewChrome = navigation.browserPreviewChrome;

  // Creation keeps the classic sidebar/chat structure while gating chrome tweaks
  // behind its own style flag so classic/workbench remain unchanged.
  const appChromeHidden = sidebarWorkbench || sidebarCreation;
  const workbenchChromeHidden = sidebarWorkbench;
  const sidebarClassName = [
    "sidebar",
    shell.sidebarCollapsed ? "sidebar--collapsed" : "",
    sidebarWorkbench ? "sidebar--workbench" : "",
  ].filter(Boolean).join(" ");
  const startupSplashHold = !activeTabId && state.meta?.ready !== true && !state.meta?.startupErr;

  const layoutStyle = useMemo(
    () =>
      ({
        "--sidebar-expanded-width": `${shellGeometry.sidebarRenderWidth}px`,
        "--chat-min-width": `${shellGeometry.chatReservedWidth}px`,
        "--workspace-width": `${shellGeometry.workspacePanelRenderWidth}px`,
        "--workspace-resizer-width": `${WORKSPACE_RESIZER_WIDTH}px`,
        "--terminal-height": `${terminalSurfaceOpen ? shell.liveTerminalHeight ?? shellGeometry.terminalRenderHeight : 0}px`,
      }) as CSSProperties,
    [shellGeometry.chatReservedWidth, shell.liveTerminalHeight, shellGeometry.sidebarRenderWidth, shellGeometry.terminalRenderHeight, terminalSurfaceOpen, shellGeometry.workspacePanelRenderWidth],
  );

  const shellClassNames = buildAppShellClassNames({
    platform: shell.desktopPlatform,
    windowsFrameless: windowsFramelessChrome,
    browserPreview: browserPreviewChrome,
    workbench: sidebarWorkbench,
    creation: sidebarCreation,
    imDetailActive: Boolean(sidebarImDetailConnection),
    sidebarCollapsed: shell.sidebarCollapsed,
    sidebarResizing: shell.sidebarResizing,
    dockGridOpen: surfaceWorkspacePanelGridOpen,
    dockOverlay: surfaceWorkspacePanelOverlay,
    terminalOpen: terminalSurfaceOpen,
    terminalResizing: shell.terminalResizing,
    dockOpen: shell.workspacePanelOpen,
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
      {sidebarWorkbench && <div className="app__dock-toggle" inert={managementActive}><DockToggleButton renderable={surfaceWorkspacePanelRenderable} t={t} onToggle={session.workspacePanelCommands.toggleWorkspacePanel} /></div>}
      <div
        ref={layoutRef}
        className={shellClassNames.layout}
        style={layoutStyle}
      >
        {!appChromeHidden && (
          <AppChrome
            platform={shell.desktopPlatform}
            browserPreviewChrome={browserPreviewChrome}
            workbenchChrome={sidebarWorkbench}
            tabs={session.visibleTabs}
            activeTabId={session.visibleTabId}
            revealActiveSignal={local.tabRevealSignal}
            commandCompact={true}
            sidebarTogglePressed={shell.sidebarTogglePressed}
            sidebarExpandBlocked={navigation.sidebarExpandBlocked}
            sidebarCollapsed={shell.sidebarCollapsed}
            sidebarToggleTitle={navigation.sidebarToggleTitle}
            workspacePanelMaximized={shell.workspacePanelMaximized}
            workspacePanelRenderable={surfaceWorkspacePanelRenderable}
            workspacePanelLabel={surfaceWorkspacePanelRenderable ? t("rightDock.collapse") : t("rightDock.expand")}
            onToggleSidebar={shellGeometry.toggleSidebar}
            onToggleWorkspacePanel={session.workspacePanelCommands.toggleWorkspacePanel}
            onTabChange={(id) => void session.tabBarCommands.handleTabChange(id)}
            onTabClose={(id) => void session.tabBarCommands.handleTabClose(id)}
            onTabsClose={(ids, nextActiveTabId) => void session.tabBarCommands.handleTabsClose(ids, nextActiveTabId)}
            onTabsReorder={(ids) => void session.tabBarCommands.handleTabsReorder(ids)}
            onNewTab={() => void navigationCommands.handleNewTab()}
            onOpenPalette={() => void navigation.paletteCommands.openPalette()}
          />
        )}
        <a className="skip-to-composer" href="#composer-input">
          {t("shortcuts.skipToComposer")}
        </a>

        <SidebarRegion {...buildSidebarRegionProps({
          automation: shell.page.kind === "automation",
          className: sidebarClassName,
          toggleTitle: navigation.sidebarToggleTitle,
          shell,
          t,
          geometry: shellGeometry,
          projectTree: {
            activeTab, imTopicSources: shell.preferences.imTopicSources, refreshSignal: local.projectRevision,
            timeFilter: local.topicTimeFilter, onTimeFilterChange: local.setTopicTimeFilter,
            searchExpanded: !sidebarCreation || shell.sidebarSearchOpen, searchFocusSignal: shell.sidebarSearchFocusSignal,
            showShortcutBadges: navigation.topicShortcuts.showTopicBadges, shortcutPlatform: shell.desktopPlatform,
            onVisibleTopicsChange: navigation.topicShortcuts.handleVisibleTopicsChange,
          },
          topics: navigation.projectTopicCommands,
          commands: {
            onNewSession: () => void navigationCommands.handleNewTab(),
            onOpenTrash: () => void navigation.historyCommands.openTrash(),
            onOpenAutomation: () => shell.openPage({ kind: "automation" }),
            onOpenSettings: chromeCommands.openSidebarSettings,
            onToggleSearch: chromeCommands.toggleSidebarSearch,
            onToggle: shellGeometry.toggleSidebar,
            onOpenTopic: navigationCommands.handleOpenTopic,
          },
        })} />

        <section className={`chat-pane${session.transcript.creationEmptyHero ? " chat-pane--creation-empty" : ""}`}>
          <TopicbarRegion view={buildTopicbarView({
            t, locale, activeTab, cwd: state.meta?.cwd, imDetail: sidebarImDetailConnection, imTopicSources: shell.preferences.imTopicSources,
            creation: sidebarCreation, chromeHidden: workbenchChromeHidden, automationReturn: shell.automationReturn,
            sidebar: { title: navigation.sidebarToggleTitle, blocked: navigation.sidebarExpandBlocked, pressed: shell.sidebarTogglePressed, collapsed: shell.sidebarCollapsed },
            rename: { editing: navigation.projectTopicCommands.topicbarEditing, draft: navigation.projectTopicCommands.topicTitleDraft },
          })} commands={{
            openAutomation: () => shell.openPage({ kind: "automation" }), toggleSidebar: shellGeometry.toggleSidebar,
            setTitleDraft: navigation.projectTopicCommands.setTopicTitleDraft, commitRename: navigation.projectTopicCommands.commitActiveTopicRename, cancelRename: navigation.projectTopicCommands.cancelActiveTopicRename,
            startRename: navigation.projectTopicCommands.startActiveTopicRename, openWorktree: navigation.worktreeMergeCommands.openWorktreeMerge,
          }}>
            <TopicbarActionsStack
              t={t}
              paletteShortcut={navigation.commandPaletteShortcut}
              onOpenPalette={() => void navigation.paletteCommands.openPalette()}
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
              creation={sidebarCreation}
              dockToggle={<DockToggleButton renderable={surfaceWorkspacePanelRenderable} t={t} onToggle={session.workspacePanelCommands.toggleWorkspacePanel} />}
            />
          </TopicbarRegion>

          <SessionStatusBanners {...buildSessionStatusBannerProps({
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
          })} />

          <ChatPaneRegion
            transitioning={runtimeTransitioning}
            t={t}
            imDetail={sidebarImDetailConnection ? {
              connection: sidebarImDetailConnection,
              onClose: () => local.setSidebarImDetailConnectionId(""),
              onOpenSettings: chromeCommands.openBotSettings,
              onManageAllowlist: chromeCommands.openBotAllowlistSettings,
              onOpenSession: (connection) => void navigationCommands.openSidebarImConnectionSession(connection),
            } : null}
            remote={activeTab?.remote ? { tab: activeTab, session: core.remoteSession } : undefined}
            transcript={{
              state,
              items: session.transcript.visibleTranscriptItems,
              tabId: session.transcript.visibleTranscriptTabId,
              geometrySessionKey: session.transcript.visibleTranscriptGeometryKey,
              footerHeight,
              revealSignal: local.transcriptRevealSignal,
              invocationMetadata: session.transcript.visibleTranscriptTabId ? session.invocation.invocationMetadataByTab[session.transcript.visibleTranscriptTabId] : undefined,
              surfaceCommitToken: core.surface.surfaceCommitToken,
              liveStore: core.liveStore,
              transcriptHydrating: session.transcript.transcriptHydrating,
              navigationDataReady: core.surface.dataReady,
              readOnly: Boolean(activeTab?.readOnly),
              controllerReady,
              hydratePlaceholderActive: session.hydratePlaceholderActive,
              clearContextPending: session.clearCommands.clearContextPending,
              creation: sidebarCreation,
              rewind: { stateActive: session.sessionUndo.rewindState != null, committing: session.sessionUndo.rewindCommitting, signal: session.sessionUndo.rewindSignal },
            }}
            onRetryHistory={() => void runtime.sessionActions.retrySessionHistory(activeTabId)}
            commands={{
              onPrompt: session.transcript.handleTranscriptPrompt,
              onDeliveryContinue: () => void session.delivery.handleDeliveryContinue(),
              onAcceptDelivery: session.controlCommands.handleAcceptDelivery,
              onOpenChanges: () => session.workspacePanelCommands.openRightDockMode("changed"),
              onOpenVerification: session.turnVerificationCommands.openTurnVerification,
              onEditPrompt: session.sessionUndo.handleEditPrompt,
              onRewind: session.sessionUndo.handleMessageAction,
              onLoadOlderHistory: session.transcript.handleLoadOlderHistory,
              onSurfacePaintReady: session.transcript.handleSurfacePaintReady,
            }}
          />

          <DecisionFooterRegion
            hidden={Boolean(sidebarImDetailConnection)}
            className={["footer", terminalSurfaceOpen && !sidebarCreation ? "footer--compact" : "", visibleDecisionSurface ? "footer--decision" : "", runtimeTransitioning ? "footer--navigation-hidden" : ""].filter(Boolean).join(" ")}
            footerRef={footerRef}
            style={core.surface.surface?.phase === "source-retained" && footerHeight > 0 ? { height: footerHeight, minHeight: footerHeight, boxSizing: "border-box" } : undefined}
            todo={footerTodo}
            undo={footerUndo}
            decision={decisionFooterSurface}
            composer={buildComposerSurface({
              view: {
                hidden: composerSurfaceHidden,
                inert: runtimeTransitioning,
                hero: session.transcript.creationEmptyHero,
                headline: t("welcome.creation.title"),
                remote: core.remoteSurfaceActive,
                rewindCommitting: session.sessionUndo.rewindCommitting,
                messageActionPending: state.messageAction != null,
                decisionActive: Boolean(decisionSurface),
                runtimeTransitioning,
                controllerReady,
                showContextWindowRing: sidebarCreation,
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
              fileRefRefreshKey: local.composerFileRefRefreshKey,
              guidance: session.transcript.latestGuidanceConsumed,
              guidanceQueuePreviewItems: navigation.guidanceQueueMockItems,
            })}
          />
        </section>

        <WorkspaceDockRegion {...buildWorkspaceDockProps({
          surface: { renderable: surfaceWorkspacePanelRenderable, overlay: surfaceWorkspacePanelOverlay, gridOpen: surfaceWorkspacePanelGridOpen },
          creation: sidebarCreation,
          remoteAvailable: shell.remoteHosts.length > 0,
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
          qualityFloor: session.profileProjection.composerProfile.qualityFloor,
          onFileTreeRefresh: local.refreshComposerFileRefs,
          onSessionRevertCommitted: session.sessionUndo.handleSessionRevertCommitted,
          onOpenInTerminal: core.remoteSurfaceActive ? undefined : session.terminalPanelCommands.openTerminalForPath,
        })} />
        <AppBottomRegions {...buildBottomRegionsProps({
          t,
          chatSurfaceVisible: session.chatSurfaceVisible,
          surfaceOpen: terminalSurfaceOpen,
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
          status: !session.statusBarVisible ? undefined : {
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
        selectionEnabled: Boolean(activeTabId && !activeTab?.readOnly && !decisionSurface && !sidebarImDetailConnection && !session.hydratePlaceholderActive),
        automationTopic: session.automation.openAutomationTopic,
        shell,
        history: navigation.historyCommands,
        navigation: navigationCommands,
        chrome: chromeCommands,
        onboarding: navigation.onboardingCommands,
        worktree: navigation.worktreeMergeCommands,
        onAddSelectedText: session.insertCommands.addSelectedTextToComposer,
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
