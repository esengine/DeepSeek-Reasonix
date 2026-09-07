import type { Todo } from "../lib/tools";
import type { RewindUndoState } from "../lib/rewindTypes";
import type { WorkspaceConflictView } from "../lib/types";
import type { DecisionSurfaceKind as MockDecisionSurfaceKind } from "../lib/decisionSurfaceMock";
import type { Translator } from "../lib/i18n";
import type { projectConversation } from "../app-runtime/conversationProjection";
import type { useSessionPromptCommands } from "../app-runtime/useSessionPromptCommands";
import type { useExtensionSurface } from "../app-runtime/useExtensionSurface";
import type { useSessionClearCommands } from "../app-runtime/useSessionClearCommands";
import type { useTabBarCommands } from "../app-runtime/useTabBarCommands";
import type { useComposerProfileProjection } from "../app-runtime/useComposerProfileProjection";
import type { useComposerInsertCommands } from "../app-runtime/useComposerInsertCommands";
import type { useComposerModeActions } from "../lib/useComposerModeActions";
import type { useComposerGoalCommands } from "../app-runtime/useComposerGoalCommands";
import type { useRemoteComposerRuntimeActions } from "../lib/useRemoteComposerIntegration";
import type { useControllerProfileCommands } from "../lib/useControllerProfileCommands";
import type {
  ApprovalProps,
  AskProps,
  ComposerProps,
  DecisionFooterRegionProps,
  DecisionFooterSurface,
  ExtensionProps,
  McpProps,
  RuntimeDecisionProps,
  TodoProps,
} from "./DecisionFooterRegion";

type SurfaceKind = MockDecisionSurfaceKind | "extension_form";
type ComposerBase = ReturnType<typeof projectConversation>["composer"];
type PromptCommands = ReturnType<typeof useSessionPromptCommands>;
type ExtensionSurfaceApi = ReturnType<typeof useExtensionSurface>;
type ClearCommands = Pick<ReturnType<typeof useSessionClearCommands>, "cancelClearContext" | "confirmClearContext">;
type TabBarApi = Pick<ReturnType<typeof useTabBarCommands>, "pendingClose" | "setPendingClose" | "resolvePendingClose" | "revealWorkspaceWriter" | "continueInDeliveryWorktree">;

/** Pure prop builders for DecisionFooterRegion; every closure keeps the exact
 *  handler identity and branching the App body previously assembled inline. */

export function buildFooterTodo(input: {
  show: boolean;
  identity: string;
  todos: Todo[];
  running: boolean;
  pendingPrompt: boolean;
  continueReady: boolean;
  onContinue: TodoProps["onContinue"];
  onDismiss: TodoProps["onDismiss"];
}): DecisionFooterRegionProps["todo"] {
  if (!input.show) return undefined;
  return {
    identity: input.identity,
    props: {
      stateKey: input.identity,
      todos: input.todos,
      running: input.running,
      pendingPrompt: input.pendingPrompt,
      onContinue: input.continueReady ? input.onContinue : undefined,
      onDismiss: input.onDismiss,
    },
  };
}

export function buildFooterUndo(input: {
  rewindState: RewindUndoState | null;
  activeTabId: string | undefined;
  onUndo: () => void;
}): DecisionFooterRegionProps["undo"] {
  const { rewindState } = input;
  if (!rewindState) return undefined;
  return {
    identity: `${input.activeTabId ?? ""}:${rewindState.transactionId ?? "rewind"}`,
    props: {
      meta: {
        turns: rewindState.turnDiff,
        filesRestored: rewindState.filesRestored ?? [],
        filesRemoved: rewindState.filesRemoved ?? [],
        onUndo: input.onUndo,
      },
    },
  };
}

export type DecisionFooterSurfaceInput = {
  view: {
    surface: SurfaceKind | null;
    activeTabId: string | undefined;
    cwd: string | undefined;
    workspaceScopeKey: string;
    approval: ApprovalProps["approval"] | null | undefined;
    ask: AskProps["ask"] | null | undefined;
    mcpInteraction: McpProps["interaction"] | null | undefined;
    extensionForm: ExtensionProps["surface"] | null | undefined;
    workspaceConflict: WorkspaceConflictView | null;
    toolApprovalMode: ApprovalProps["toolApprovalMode"];
    insertRequest: ApprovalProps["insertRequest"];
  };
  prompts: PromptCommands;
  extension: ExtensionSurfaceApi;
  tabs: TabBarApi;
  clear: ClearCommands;
  onStop: () => void;
  cancelWorkspaceConflict: RuntimeDecisionProps["onCancel"];
  onOpenLink: McpProps["onOpenLink"];
  onRevisionActiveChange: ApprovalProps["onRevisionActiveChange"];
  t: Translator;
};

export function buildDecisionFooterSurface(input: DecisionFooterSurfaceInput): DecisionFooterSurface | undefined {
  const { view, prompts, extension, tabs, clear, t } = input;
  const { surface, activeTabId } = view;
  if ((surface === "tool_approval" || surface === "plan_approval") && view.approval) {
    return {
      kind: "approval",
      identity: `${activeTabId ?? ""}:${view.approval.id}`,
      props: {
        approval: view.approval,
        cwd: view.cwd,
        tabId: activeTabId,
        workspaceScopeKey: view.workspaceScopeKey,
        insertRequest: view.insertRequest,
        onRevisionActiveChange: input.onRevisionActiveChange,
        onAnswer: prompts.handleApprovalAnswer,
        onResolveRecovery: prompts.handleRecoveryAnswer,
        onRevisePlan: prompts.handleRevisePlan,
        onExitPlan: prompts.handleExitPlan,
        onStop: input.onStop,
        toolApprovalMode: view.toolApprovalMode,
      },
    };
  }
  if (surface === "ask" && view.ask) {
    return {
      kind: "ask",
      identity: `${activeTabId ?? ""}:${view.ask.id}`,
      props: {
        ask: view.ask,
        onAnswer: prompts.handleQuestionAnswer,
        onDismiss: prompts.handleQuestionDismiss,
        onStop: input.onStop,
      },
    };
  }
  if (surface === "mcp_interaction" && view.mcpInteraction) {
    return {
      kind: "mcp",
      identity: `${activeTabId ?? ""}:${view.mcpInteraction.id}`,
      props: {
        interaction: view.mcpInteraction,
        busy: false,
        onAnswer: prompts.handleMCPAnswer,
        onOpenLink: input.onOpenLink,
      },
    };
  }
  if (surface === "extension_form" && view.extensionForm) {
    return {
      kind: "extension",
      identity: `${activeTabId ?? ""}:${view.extensionForm.pluginId}:${view.extensionForm.surfaceId}`,
      props: {
        surface: view.extensionForm,
        busy: extension.extensionFormBusy,
        onSubmit: (values) => void extension.submitExtensionForm(values),
        onCancel: () => void extension.cancelExtensionForm(),
      },
    };
  }
  if (surface === "workspace_conflict" && view.workspaceConflict) {
    const workspaceConflict = view.workspaceConflict;
    return {
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
        onCancel: input.cancelWorkspaceConflict,
        actions: [
          ...(workspaceConflict.canReveal ? [{
            key: "1", label: t("runtime.revealWriter"), description: t("runtime.revealWriterDesc"),
            onClick: () => void tabs.revealWorkspaceWriter(),
          }] : []),
          ...(workspaceConflict.canCreateWorktree ? [{
            key: "2", label: t("runtime.openWorktree"), description: t("runtime.openWorktreeDesc"),
            onClick: () => void tabs.continueInDeliveryWorktree(),
          }] : []),
        ],
        secondaryAction: {
          key: "Esc", label: t("runtime.cancelWait"), description: t("runtime.cancelWaitDesc"),
          onClick: input.cancelWorkspaceConflict,
        },
      },
    };
  }
  if (surface === "close_active" && tabs.pendingClose) {
    const pendingClose = tabs.pendingClose;
    return {
      kind: "runtime",
      identity: "close-active",
      props: {
        id: "close-active",
        title: t("runtime.closeTitle"),
        badge: t("status.jobs", { n: pendingClose.work.jobs.length }),
        meta: t("runtime.closeMeta"),
        onCancel: () => tabs.setPendingClose(null),
        actions: [
          {
            key: "1", label: t("runtime.keepRunning"), description: t("runtime.keepRunningDesc"),
            onClick: () => void tabs.resolvePendingClose("keep_running"), disabled: pendingClose.stopping,
          },
          {
            key: "2", label: pendingClose.stopping ? t("status.jobStopping") : t("runtime.stopAndClose"),
            description: t("runtime.stopAndCloseDesc"), onClick: () => void tabs.resolvePendingClose("stop_and_close"),
            danger: true, disabled: pendingClose.stopping,
          },
        ],
        secondaryAction: {
          key: "Esc", label: t("runtime.returnToTask"), description: t("runtime.closeCancelDesc"),
          onClick: () => tabs.setPendingClose(null), disabled: pendingClose.stopping,
        },
      },
    };
  }
  if (surface === "clear_context") {
    return {
      kind: "clear-context",
      identity: "clear-context",
      props: { onCancel: clear.cancelClearContext, onConfirm: () => void clear.confirmClearContext() },
    };
  }
  return undefined;
}

export type ComposerSurfaceInput = {
  view: {
    hidden: boolean;
    inert: boolean;
    hero: boolean;
    headline: string;
    remote: boolean;
    rewindCommitting: boolean;
    messageActionPending: boolean;
    decisionActive: boolean;
    runtimeTransitioning: boolean;
    controllerReady: boolean;
    showContextWindowRing: boolean;
  };
  base: ComposerBase;
  tab: { readOnly?: boolean; floorInferred?: boolean } | undefined;
  tabId: string | undefined;
  profile: ReturnType<typeof useComposerProfileProjection>;
  router: { handleSend: ComposerProps["onSend"]; handleSteer: ComposerProps["onSteer"] };
  modes: ReturnType<typeof useComposerModeActions>;
  goals: ReturnType<typeof useComposerGoalCommands>;
  remoteGoal: ReturnType<typeof useRemoteComposerRuntimeActions>;
  modelSwitch: Pick<ReturnType<typeof useControllerProfileCommands>, "switchModelFromUi">;
  inserts: Pick<ReturnType<typeof useComposerInsertCommands>, "composerInsertRequest" | "selectedTextRequest">;
  control: { handleCancelActive: ComposerProps["onCancel"] };
  remoteComposer: {
    send: ComposerProps["onSend"];
    cancel: ComposerProps["onCancel"];
    ready: boolean;
    profileReady: boolean;
    liveStore: ComposerProps["liveStore"];
  };
  localLiveStore: ComposerProps["liveStore"];
  onInvocationMetadataChange: ComposerProps["onInvocationMetadataChange"];
  onCycleMode: ComposerProps["onCycleMode"];
  transientDismissSignal: ComposerProps["transientDismissSignal"];
  sessionKey: ComposerProps["sessionKey"];
  workspaceScopeKey: ComposerProps["workspaceScopeKey"];
  fileRefRefreshKey: ComposerProps["fileRefRefreshKey"];
  guidance: { key: string; itemId?: string; text: string } | null;
  guidanceQueuePreviewItems: ComposerProps["guidanceQueuePreviewItems"];
};

export function buildComposerSurface(input: ComposerSurfaceInput): DecisionFooterRegionProps["composer"] {
  const { base, view, profile, router, modes, goals, remoteGoal, modelSwitch, inserts, control, remoteComposer } = input;
  return {
    hidden: view.hidden,
    inert: view.inert,
    hero: view.hero,
    headline: view.headline,
    props: {
      ...base,
      running: base.running || (!view.remote && view.rewindCommitting),
      collaborationMode: profile.collaborationMode,
      toolApprovalMode: profile.toolApprovalMode,
      qualityFloor: profile.composerProfile.qualityFloor,
      floorInferred: (input.tab?.floorInferred ?? false) && !profile.composerProfile.pending.qualityFloor,
      onSetQualityFloor: profile.applyQualityFloor,
      goal: profile.goal,
      tabId: input.tabId,
      onSend: view.remote ? remoteComposer.send : router.handleSend,
      onInvocationMetadataChange: input.onInvocationMetadataChange,
      onSteer: router.handleSteer,
      onCancel: view.remote ? remoteComposer.cancel : control.handleCancelActive,
      onCycleMode: input.onCycleMode,
      onSetMode: modes.applyMode,
      onSetCollaborationMode: goals.setCollaborationModeFromUi,
      onSetToolApprovalMode: modes.applyToolApprovalMode,
      onToggleYoloApprovalMode: modes.toggleYoloApprovalMode,
      onClearGoal: goals.clearGoalFromUi,
      onPauseGoal: remoteGoal.pauseGoal,
      onResumeGoal: remoteGoal.resumeGoal,
      onSwitchModel: modelSwitch.switchModelFromUi,
      onSetEffort: remoteGoal.setEffort,
      insertRequest: inserts.composerInsertRequest,
      selectedTextRequest: inserts.selectedTextRequest,
      readOnly: Boolean(input.tab?.readOnly),
      disabled: view.runtimeTransitioning || view.rewindCommitting || view.messageActionPending || view.decisionActive,
      submitDisabled: view.remote ? !remoteComposer.ready || !remoteComposer.profileReady : !view.controllerReady,
      decisionPending: view.rewindCommitting || view.messageActionPending || view.decisionActive,
      ready: view.remote ? remoteComposer.ready && remoteComposer.profileReady : view.controllerReady,
      liveStore: view.remote ? remoteComposer.liveStore : input.localLiveStore,
      suspendedByDecision: view.decisionActive,
      transientDismissSignal: input.transientDismissSignal,
      sessionKey: input.sessionKey,
      workspaceScopeKey: input.workspaceScopeKey,
      fileRefRefreshKey: input.fileRefRefreshKey,
      guidanceConsumedKey: input.guidance?.key,
      guidanceConsumedItemId: input.guidance?.itemId,
      guidanceConsumedText: input.guidance?.text,
      guidanceQueuePreviewItems: input.guidanceQueuePreviewItems,
      showContextWindowRing: view.showContextWindowRing,
      heroMode: view.hero,
    },
  };
}
