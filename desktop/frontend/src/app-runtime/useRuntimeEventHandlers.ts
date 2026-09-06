import { useEffect, useRef, type Dispatch, type RefObject, type SetStateAction } from "react";
import { app, onProjectTreeChanged } from "../lib/bridge";
import { useCommittedCommand } from "../lib/useCommittedCommand";
import { asArray } from "../lib/array";
import { createBoundedRefreshCoordinator, sameTabMetaLists, seedActiveTabMetaList, shouldRefreshTabMetaForEvent, TAB_META_MAX_IN_FLIGHT } from "../lib/tabMetaRefresh";
import { clearAttentionChimeKeys, playAttentionChime, playSuccessChime, shouldPlayAttentionChimeForEvent } from "../lib/sound";
import { composerProfileFromTab, defaultComposerProfile, patchComposerProfile, resolvePlanRestoreTabId, shouldRestoreUserPlanModeForProfile, updateUserPlanModeIntent, type ComposerProfile, type UserPlanModeIntents } from "../lib/composerProfile";
import { useRemoteTabOpened } from "../lib/useRemoteTabOpened";
import { recordFrontendDiagnostic } from "../lib/frontendDiagnosticBridge";
import { useRemoteStore } from "../store/remote";
import type { TabMeta } from "../lib/types";
import type {
  RemoteForwardsListener,
  RemoteServerListener,
  RemoteStatusListener,
  RuntimeEventListener,
  RuntimeReadyListener,
  RuntimeRebuiltListener,
} from "./AppRuntimeEffects";

export type RuntimeEventHandlersInput = {
  activeTabId: string | undefined;
  workspaceScopeKey: string;
  activeTabIdRef: RefObject<string | undefined>;
  workspaceScopeActiveTabRef: RefObject<string | undefined>;
  userPlanModeByTabRef: RefObject<UserPlanModeIntents>;
  setTabMetas: Dispatch<SetStateAction<TabMeta[]>>;
  setTabOrderIds: Dispatch<SetStateAction<string[]>>;
  setComposerProfilesByTab: Dispatch<SetStateAction<Record<string, ComposerProfile>>>;
  setDockRefreshKey: Dispatch<SetStateAction<number>>;
  setProjectRevision: Dispatch<SetStateAction<number>>;
  setWorkspaceControllerEpoch: Dispatch<SetStateAction<number>>;
  setControllerCollaborationMode(mode: string): Promise<void>;
};

/**
 * Owns the runtime event surface: tab-meta registry refresh/seed/remote
 * registration with its single-flight coordinator, the runtime
 * event/ready/rebuilt listeners (chimes, plan-mode restore, workspace-scope
 * epochs), the remote status/forwards/server listeners, and the workspace
 * focus reconciliation that refreshes tab metas when the project tree changes.
 */
export function useRuntimeEventHandlers(input: RuntimeEventHandlersInput) {
  const { activeTabId, workspaceScopeKey, setProjectRevision } = input;
  const attentionChimeEvents = useRef(new Set<string>());
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
      input.setTabMetas((current) => sameTabMetaLists(current, tabs) ? current : tabs);
    }
    return tabs;
  });
  const seedActiveTabMeta = useCommittedCommand((tab: TabMeta): void => {
    input.setTabMetas((current) => seedActiveTabMetaList(current, tab));
    input.setTabOrderIds((current) => current.includes(tab.id) ? current : [...current, tab.id]);
  });
  const updateRemoteTabMeta = useCommittedCommand((tab: TabMeta): void => {
    input.setTabMetas((current) => current.map((existing) => existing.id === tab.id
      ? { ...existing, ...tab, active: existing.active }
      : existing));
  });

  const registerRemoteTabMeta = useCommittedCommand((tab: TabMeta) => {
    input.setTabMetas(current => current.some(existing => existing.id === tab.id) ? current : [...current, { ...tab, active: false }]);
  });
  useRemoteTabOpened(registerRemoteTabMeta, updateRemoteTabMeta);

  const handleRuntimeEvent = useCommittedCommand<RuntimeEventListener>((event) => {
    recordFrontendDiagnostic("runtime", "runtime.event", { action: event.kind, status: event.err ? "error" : "ok" });
    if (event.kind === "turn_done") {
      input.setDockRefreshKey((value) => value + 1);
      input.setProjectRevision((value) => value + 1);
      if (!event.err) playSuccessChime();
    }
    if (shouldPlayAttentionChimeForEvent(event, attentionChimeEvents.current)) playAttentionChime();
    if (shouldRefreshTabMetaForEvent(event.kind)) void refreshTabMetas(undefined, { afterMutation: true });
    if (event.kind !== "turn_done") return;
    const turnTabId = resolvePlanRestoreTabId(event.tabId, input.activeTabIdRef.current);
    void refreshTabMetas(undefined, { afterMutation: true }).then((tabs) => {
      if (!turnTabId) return;
      const tab = tabs.find((item) => item.id === turnTabId);
      const baseProfile = tab ? composerProfileFromTab(tab) : defaultComposerProfile;
      if (!shouldRestoreUserPlanModeForProfile(input.userPlanModeByTabRef.current, turnTabId, baseProfile)) {
        if (baseProfile.goal.trim()) {
          input.userPlanModeByTabRef.current = updateUserPlanModeIntent(input.userPlanModeByTabRef.current, turnTabId, false);
        }
        return;
      }
      input.setComposerProfilesByTab((current) => patchComposerProfile(
        current, turnTabId, current[turnTabId] ?? baseProfile,
        { collaborationMode: "plan", goalDraftMode: false, goal: "" },
        ["collaborationMode", "goal"],
      ));
      if (input.activeTabIdRef.current === turnTabId) void input.setControllerCollaborationMode("plan");
    });
  });

  const handleRuntimeReady = useCommittedCommand<RuntimeReadyListener>((readyTabId) => {
    recordFrontendDiagnostic("runtime", "runtime.ready", { ready: true, hasActiveTab: Boolean(readyTabId) });
    clearAttentionChimeKeys(attentionChimeEvents.current, readyTabId);
    void refreshTabMetas();
    if (!readyTabId || readyTabId === input.workspaceScopeActiveTabRef.current) {
      input.setWorkspaceControllerEpoch((value) => value + 1);
    }
  });

  const handleRuntimeRebuilt = useCommittedCommand<RuntimeRebuiltListener>((rebuiltTabId) => {
    recordFrontendDiagnostic("runtime", "runtime.rebuilt", { ready: true, hasActiveTab: Boolean(rebuiltTabId) });
    clearAttentionChimeKeys(attentionChimeEvents.current, rebuiltTabId);
    if (!rebuiltTabId || rebuiltTabId === input.workspaceScopeActiveTabRef.current) {
      input.setWorkspaceControllerEpoch((value) => value + 1);
    }
  });

  useEffect(() => {
    let live = true;
    const ready = import("../lib/workspaceRefreshStore")
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
  }, [activeTabId, refreshTabMetas, setProjectRevision, workspaceScopeKey]);

  const handleRemoteStatus = useCommittedCommand<RemoteStatusListener>((status) => {
    useRemoteStore.getState().applyStatus(status);
    if (status.state === "stopped" && status.error) useRemoteStore.getState().requestStatusPopover(status.hostId);
  });
  const handleRemoteForwards = useCommittedCommand<RemoteForwardsListener>((event) => useRemoteStore.getState().setForwards(event.hostId, event.forwards));
  const handleRemoteServer = useCommittedCommand<RemoteServerListener>((server) => useRemoteStore.getState().setServer(server));
  const handleInitialRemoteHosts = useCommittedCommand((hosts: Awaited<ReturnType<typeof app.RemoteHosts>>) => useRemoteStore.getState().setHosts(hosts));
  const handleInitialRemoteStatuses = useCommittedCommand((statuses: Awaited<ReturnType<typeof app.RemoteConnectionStatuses>>) => useRemoteStore.getState().hydrateStatuses(statuses));

  return {
    refreshTabMetas,
    seedActiveTabMeta,
    registerRemoteTabMeta,
    updateRemoteTabMeta,
    handleRuntimeEvent,
    handleRuntimeReady,
    handleRuntimeRebuilt,
    handleRemoteStatus,
    handleRemoteForwards,
    handleRemoteServer,
    handleInitialRemoteHosts,
    handleInitialRemoteStatuses,
  };
}
