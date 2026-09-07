import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useCommittedCommand } from "./lib/useCommittedCommand";
import { openExternal } from "./lib/bridge";
import { useT, useI18n } from "./lib/i18n";
import { useToast } from "./lib/toast";
import { useGoalActionHandler } from "./lib/goalAction";
import { useActiveRemoteSession } from "./lib/useRemoteSession";
import { useWarmTerminalPanel } from "./lib/useWarmTerminalPanel";
import { setReasoningDisplayPending } from "./lib/reasoningDisplayPreference";
import type { RestorableToolApprovalMode } from "./lib/toolApprovalMode";
import type { ComposerProfile, UserPlanModeIntents } from "./lib/composerProfile";
import type { TabMeta } from "./lib/types";
import type { HistoryViewState } from "./app-runtime/historyViewProjection";
import { useNavigationSurface } from "./lib/useNavigationSurface";
import { projectNavigationSurfaceTarget } from "./app-runtime/conversationProjection";
import { useSessionOperations } from "./app-runtime/useSessionOperations";
import { createSessionSurfaceFence, sessionIdentityKey } from "./app-runtime/sessionTarget";
import { commitAppRenderToken, createAppRenderToken } from "./app-runtime/appLifecycleProbe";
import { useAppRuntimeAdapter } from "./app-runtime/useAppRuntimeAdapter";
import { useAppShellStores } from "./app-runtime/useAppShellStores";
import { useAppSessionComposition } from "./app-runtime/useAppSessionComposition";
import { useAppNavigationComposition } from "./app-runtime/useAppNavigationComposition";
import { useTopicTimeFilter } from "./app-runtime/useLocalUiLifecycles";
import { AppRuntimeView } from "./app-shell/AppRuntimeView";

// Hold reasoning UI until the authoritative desktop startup settings arrive;
// this prevents a hidden preference from flashing content during first paint.
setReasoningDisplayPending();

/**
 * Composition root: owns the controller adapter, the session identity/fence,
 * the navigation surface and every store-backed state, then delegates all
 * command domains to the session/navigation compositions and the tree to the
 * shell view. Wiring only — no domain logic lives here.
 */
export function AppRuntime() {
  const appRenderToken = createAppRenderToken();
  useLayoutEffect(() => commitAppRenderToken(appRenderToken));
  const runtime = useAppRuntimeAdapter();
  const { state, liveStore, activeTabId, notice } = runtime.snapshot;
  const t = useT();
  const { locale } = useI18n();
  const { showToast } = useToast();
  const { runGoalAction, handleGoalActionError } = useGoalActionHandler();
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
  const navigationSurface = useNavigationSurface(projectNavigationSurfaceTarget({
    activeTabId, sessionKey: activeSessionIdentity, local: state, remote: remoteSurfaceActive ? remoteSession : undefined,
  }));
  const shell = useAppShellStores();
  const [tabRevealSignal, setTabRevealSignal] = useState(0);
  const [transcriptRevealSignal, setTranscriptRevealSignal] = useState(0);
  const [histView, setHistView] = useState<HistoryViewState | null>(null);
  const [sidebarImDetailConnectionId, setSidebarImDetailConnectionId] = useState("");
  const [topicTimeFilter, setTopicTimeFilter] = useTopicTimeFilter();
  const [tasksOpen, setTasksOpen] = useState<false | "session" | "all">(false);
  const workspaceScopeActiveTabRef = useRef(activeTabId);
  const [workspaceControllerEpoch, setWorkspaceControllerEpoch] = useState(0);
  workspaceScopeActiveTabRef.current = activeTabId;
  const { mounted: terminalContentVisible, fitEnabled: terminalFitEnabled, prefetch: prefetchTerminalPanel } = useWarmTerminalPanel(shell.terminalPanelOpen, shell.terminalResizing, !shell.managementActive);
  const [dockRefreshKey, setDockRefreshKey] = useState(0);
  const [fileRefRefreshKey, setFileRefRefreshKey] = useState(0);
  const refreshComposerFileRefs = useCommittedCommand(() => setFileRefRefreshKey((value) => value + 1));
  const composerFileRefRefreshKey = `${dockRefreshKey}:${fileRefRefreshKey}`;
  const [projectRevision, setProjectRevision] = useState(0);

  const session = useAppSessionComposition({
    runtime,
    t,
    showToast,
    shell,
    core: {
      state, liveStore, activeTabId, notice, activeTab, remoteSurfaceActive, remoteSession, remoteComposerReady,
      remoteSend, remoteCancel, activeSessionIdentity, sessionSurfaceFence, sessionOperations,
    },
    surface: navigationSurface,
    stores: {
      composerProfilesByTab, setComposerProfilesByTab, tabMetas, setTabMetas, tabOrderIds, setTabOrderIds,
      yoloRestoreToolApprovalModesRef, userPlanModeByTabRef,
    },
    local: {
      setHistView, setTabRevealSignal, setTranscriptRevealSignal,
      sidebarImDetailConnectionId, setSidebarImDetailConnectionId,
      workspaceScopeActiveTabRef, workspaceControllerEpoch, setWorkspaceControllerEpoch,
      dockRefreshKey, setDockRefreshKey, fileRefRefreshKey, setFileRefRefreshKey, projectRevision, setProjectRevision,
    },
    goal: { runGoalAction, handleGoalActionError },
  });
  const navigation = useAppNavigationComposition({
    runtime,
    t,
    notice,
    showToast,
    shell,
    state,
    activeTab,
    activeTabId,
    activeSessionIdentity,
    remoteSurfaceActive,
    surface: navigationSurface,
    local: {
      setHistView, setProjectRevision,
      setSidebarImDetailConnectionId, setTasksOpen,
    },
    session,
  });

  return (
    <AppRuntimeView
      core={{
        state, activeTab, activeTabId, liveStore, remoteSurfaceActive, remoteSession, remoteComposerReady,
        remoteCancel, surface: navigationSurface, t, locale, onOpenLink: openExternal,
      }}
      shell={shell}
      session={session}
      navigation={navigation}
      runtime={runtime}
      local={{
        tasksOpen, setTasksOpen, topicTimeFilter, setTopicTimeFilter,
        sidebarImDetailConnectionId, setSidebarImDetailConnectionId,
        tabRevealSignal, transcriptRevealSignal, histView,
        projectRevision, dockRefreshKey, composerFileRefRefreshKey, refreshComposerFileRefs,
        terminalContentVisible, terminalFitEnabled, prefetchTerminalPanel,
      }}
    />
  );
}
