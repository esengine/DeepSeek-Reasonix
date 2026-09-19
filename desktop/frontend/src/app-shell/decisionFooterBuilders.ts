import type { Todo } from "../lib/tools";
import { modelSettingsAllowSubmission } from "../lib/authenticationTypes";
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
import { draftSubmissionLocksEditing, draftSurfaceNeedsAttention, type useSessionDraftSurface } from "../app-runtime/useSessionDraftSurface";
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
      identity: prompts.approvalTarget?.instanceKey ?? `${activeTabId ?? ""}:${view.approval.id}`,
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
      identity: prompts.questionTarget?.instanceKey ?? `${activeTabId ?? ""}:${view.ask.id}`,
      props: {
        ask: view.ask,
        draftScope: prompts.questionTarget?.instanceKey ?? `${activeTabId ?? ""}:${view.ask.id}`,
        onAnswer: prompts.handleQuestionAnswer,
        onDismiss: prompts.handleQuestionDismiss,
        onStop: input.onStop,
      },
    };
  }
  if (surface === "mcp_interaction" && view.mcpInteraction) {
    return {
      kind: "mcp",
      identity: prompts.mcpTarget?.instanceKey ?? `${activeTabId ?? ""}:${view.mcpInteraction.id}`,
      props: {
        interaction: view.mcpInteraction,
        instanceKey: prompts.mcpTarget?.instanceKey ?? `${activeTabId ?? ""}:${view.mcpInteraction.id}`,
        busy: false,
        onAnswer: prompts.handleMCPAnswer,
        onOpenLink: input.onOpenLink,
      },
    };
  }
  if (surface === "extension_form" && view.extensionForm) {
    return {
      kind: "extension",
      identity: `${activeTabId ?? ""}:${view.extensionForm.pluginId}:${view.extensionForm.surfaceId}:${view.extensionForm.formInstanceId}`,
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
    submitDisabledReason?: string;
    draftHint?: string;
  };
  base: ComposerBase;
  tab: { readOnly?: boolean; sessionPath?: string; workspaceRoot?: string; authentication?: ComposerProps["authentication"]; modelSettingsPending?: boolean; remote?: { hostId: string; workspace: string } } | undefined;
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
  workspaceContext: ComposerProps["workspaceContext"];
  fileRefRefreshKey: ComposerProps["fileRefRefreshKey"];
  guidance: { key: string; itemId?: string; text: string } | null;
  guidanceQueuePreviewItems: ComposerProps["guidanceQueuePreviewItems"];
  draft?: ReturnType<typeof useSessionDraftSurface>;
};

export function buildComposerSurface(input: ComposerSurfaceInput): DecisionFooterRegionProps["composer"] {
  const { base, view, profile, router, modes, goals, remoteGoal, modelSwitch, inserts, control, remoteComposer } = input;
  const surface: DecisionFooterRegionProps["composer"] = {
    hidden: view.hidden,
    inert: view.inert,
    hero: view.hero,
    headline: view.headline,
    props: {
      ...base,
      running: base.running || (!view.remote && view.rewindCommitting),
      collaborationMode: profile.collaborationMode,
      toolApprovalMode: profile.toolApprovalMode,
      goal: profile.goal,
      tabId: input.tabId,
      workspaceRoot: input.tab?.workspaceRoot,
      onSend: view.remote ? remoteComposer.send : router.handleSend,
      onInvocationMetadataChange: input.onInvocationMetadataChange,
      onSteer: router.handleSteer,
      onCancel: view.remote ? remoteComposer.cancel : control.handleCancelActive,
      onCycleMode: input.onCycleMode,
      onSetMode: modes.applyMode,
      onSetCollaborationMode: goals.setCollaborationModeFromUi,
      onSetToolApprovalMode: modes.applyToolApprovalMode,
      onClearGoal: goals.clearGoalFromUi,
      onEditGoal: goals.editGoalFromUi,
      onPauseGoal: remoteGoal.pauseGoal,
      onResumeGoal: remoteGoal.resumeGoal,
      onSwitchModel: modelSwitch.switchModelFromUi,
      onSetEffort: remoteGoal.setEffort,
      insertRequest: inserts.composerInsertRequest,
      selectedTextRequest: inserts.selectedTextRequest,
      readOnly: Boolean(input.tab?.readOnly),
      disabled: view.runtimeTransitioning || view.rewindCommitting || view.messageActionPending || view.decisionActive,
      submitDisabled: view.remote ? !remoteComposer.ready || !remoteComposer.profileReady : !modelSettingsAllowSubmission(view.controllerReady, input.tab),
      submitDisabledReason: view.submitDisabledReason,
      authentication: view.remote ? undefined : input.tab?.authentication,
      decisionPending: view.rewindCommitting || view.messageActionPending || view.decisionActive,
      ready: view.remote ? remoteComposer.ready && remoteComposer.profileReady : view.controllerReady,
      liveStore: view.remote ? remoteComposer.liveStore : input.localLiveStore,
      suspendedByDecision: view.decisionActive,
      transientDismissSignal: input.transientDismissSignal,
      sessionKey: input.sessionKey,
      inboxSessionPath: input.tab?.sessionPath,
      inboxHostId: input.tab?.remote?.hostId,
      inboxWorkspace: input.tab?.remote?.workspace,
      workspaceScopeKey: input.workspaceScopeKey,
      // Match the workspace launcher's ownership boundary: project/branch
      // selection configures a new empty session. Once the transcript has
      // content, the session keeps its established workspace and the composer
      // returns to the compact follow-up layout.
      workspaceContext: view.hero || input.draft?.surface ? input.workspaceContext : undefined,
      fileRefRefreshKey: input.fileRefRefreshKey,
      guidanceConsumedKey: input.guidance?.key,
      guidanceConsumedItemId: input.guidance?.itemId,
      guidanceConsumedText: input.guidance?.text,
      guidanceQueuePreviewItems: input.guidanceQueuePreviewItems,
      showContextWindowRing: view.showContextWindowRing,
      heroMode: view.hero && view.showContextWindowRing,
    },
  };
  const draft = input.draft?.surface;
  if (!draft || !input.draft) return surface;
  const draftController = input.draft;
  const operationActive = draft.preparingSubmission || draftSubmissionLocksEditing(draft.operation);
  const needsAttention = draftSurfaceNeedsAttention(draft);
  return {
    hidden: false,
    inert: false,
    hero: !needsAttention,
    headline: input.view.headline,
    hint: needsAttention ? undefined : input.view.draftHint,
    props: {
      ...surface.props,
      running: operationActive && draft.operation?.phase !== "accepted",
      collaborationMode: (draft.settings.collaborationMode || "normal") as ComposerProps["collaborationMode"],
      toolApprovalMode: (draft.settings.toolApprovalMode || "ask") as ComposerProps["toolApprovalMode"],
      goal: draft.settings.goal,
      cwd: draft.draft.workspaceRoot,
      workspaceRoot: draft.draft.workspaceRoot,
      modelLabel: draft.settings.model,
      commandCatalog: draft.commands,
      tabId: undefined,
      onCaptureSubmit: () => draftController.captureSubmission(draft.draft.id, draft.generation),
      onReleaseSubmit: draftController.releasePreparation,
      onPrepareSubmit: draftController.flushPreparation,
      onSend: (display, submit, tabId, structured, capture) => (
        draftController.submitFrom(draft.draft.id, draft.generation, display, submit, tabId, structured, capture)
      ),
      onSteer: undefined,
      onCycleMode: () => draftController.updateSettingsFor(draft.draft.id, draft.generation, { collaborationMode: draft.settings.collaborationMode === "plan" ? "normal" : "plan" }),
      readOnly: false,
      attachmentInputEnabled: true,
      imageInputEnabled: draft.models?.find(model => model.ref === draft.settings.model)?.vision ?? false,
      imageUnderstandingEnabled: false,
      onCancel: async () => {
        await draftController.cancelSubmission();
        return { discardedItemIds: [] };
      },
      onSetMode: (mode) => draftController.updateSettingsFor(draft.draft.id, draft.generation, { mode }),
      onSetCollaborationMode: (collaborationMode) => draftController.updateSettingsFor(draft.draft.id, draft.generation, { collaborationMode }),
      onSetToolApprovalMode: (toolApprovalMode) => draftController.updateSettingsFor(draft.draft.id, draft.generation, { toolApprovalMode }),
      onClearGoal: () => draftController.updateSettingsFor(draft.draft.id, draft.generation, { goal: "", collaborationMode: "normal" }),
      onEditGoal: (goal) => draftController.updateSettingsFor(draft.draft.id, draft.generation, { goal, collaborationMode: goal ? "goal" : "normal" }),
      onPauseGoal: () => {},
      onResumeGoal: () => {},
      onSwitchModel: (model) => { draftController.updateSettingsFor(draft.draft.id, draft.generation, { model }); return true; },
      onSetEffort: (effort) => draftController.updateSettingsFor(draft.draft.id, draft.generation, { effort }),
      effort: {
        supported: true,
        current: draft.settings.effort || "auto",
        default: "auto",
        levels: ["auto", "low", "medium", "high", "max"],
      },
      disabled: operationActive,
      submitDisabled: draft.saveState === "conflict" || draft.pendingTasks > 0 || operationActive,
      submitDisabledReason: draft.saveState === "conflict" ? "Resolve the draft conflict before sending." : undefined,
      decisionPending: operationActive,
      ready: true,
      liveStore: undefined,
      suspendedByDecision: false,
      sessionKey: `draft:${draft.draft.id}`,
      inboxSessionPath: undefined,
      inboxHostId: undefined,
      inboxWorkspace: undefined,
      workspaceScopeKey: `draft:${draft.draft.workspaceId}`,
      workspaceContext: surface.props.workspaceContext ? {
        ...surface.props.workspaceContext,
        scope: draft.draft.scope === "project" ? "project" : "global",
        workspaceRoot: draft.draft.workspaceRoot,
        scopeKey: `draft:${draft.draft.workspaceId}`,
        remote: false,
      } : undefined,
      persistentDraft: {
        draftId: draft.draft.id,
        generation: draft.generation,
        initial: draft.content,
        revision: draft.draft.revision,
        onChange: draftController.updateContentFor,
        onPatch: draftController.patchContentFor,
        isCurrent: draftController.isCurrentHandle,
        canEdit: draftController.canEditHandle,
        trackTask: draftController.trackTask,
        onTaskError: draftController.reportTaskError,
      },
      composerTarget: { kind: "draft", draftId: draft.draft.id },
    },
  };
}
