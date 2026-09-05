import type { State } from "../lib/useController";
import type { RemoteSessionApi } from "../lib/useRemoteSession";
import type { BackgroundRuntimeView, TabMeta } from "../lib/types";

type Input = {
  local: State;
  remote?: Pick<RemoteSessionApi, "transcript" | "running" | "modelLabel" | "commands" | "composerProfile" | "goalRuntime" | "effort">;
  tab?: Pick<TabMeta, "id" | "label" | "remote" | "workspaceName">;
  activeTabId?: string;
  backgroundRuntimes: BackgroundRuntimeView[];
  connectingLabel: string;
};

/** Readiness and paint identity must come from the surface that will render. */
export function projectNavigationSurfaceTarget(input: {
  activeTabId?: string; sessionKey: string;
  local: Pick<State, "meta" | "backendActivationPending" | "hydrating" | "hydrateError">;
  remote?: Pick<RemoteSessionApi, "state" | "hydrated" | "error" | "surfaceGeneration">;
}) {
  const { remote, local } = input;
  const terminal = remote && ["error", "serve_down", "disconnected"].includes(remote.state);
  return {
    activeTabId: input.activeTabId,
    sessionKey: remote ? JSON.stringify([input.sessionKey, remote.surfaceGeneration]) : input.sessionKey,
    ready: remote ? remote.state === "ready" && remote.hydrated : local.meta?.ready === true,
    backendActivationPending: remote ? false : Boolean(local.backendActivationPending),
    hydrating: remote ? !remote.hydrated && !terminal : Boolean(local.hydrating),
    hydrateError: remote ? terminal ? remote.error || remote.state : undefined : local.hydrateError,
  };
}

/** Display-only projection. No local telemetry fallback is permitted on a remote surface. */
export function projectConversation({ local, remote, tab, activeTabId, backgroundRuntimes, connectingLabel }: Input) {
  const runtime = remote?.transcript ?? local;
  const remoteActive = Boolean(remote);
  const modelLabel = remote ? remote.modelLabel || tab?.label : local.meta?.label;
  const timing = {
    turnPhase: runtime.turnPhase, turnStartAt: runtime.turnStartAt,
    turnWaitAccumMs: runtime.turnWaitAccumMs, promptWaitStartedAt: runtime.promptWaitStartedAt,
    turnTokens: runtime.turnTokens, turnOutputTokens: runtime.turnOutputTokens,
    turnOutputCharsAtUsage: runtime.turnOutputCharsAtUsage,
    turnModelActiveAt: runtime.turnModelActiveAt, turnModelActiveMs: runtime.turnModelActiveMs,
    turnArgChars: runtime.turnArgChars, retry: runtime.retry,
  };
  return {
    runtime,
    localToolsEnabled: !remoteActive,
    composer: {
      ...timing,
      running: remote ? remote.running : local.running,
      goalStatus: remote ? remote.composerProfile?.goalStatus : local.meta?.goalStatus,
      goalRuntime: remote ? remote.goalRuntime : local.meta?.goalRuntime,
      cwd: remote ? tab?.remote?.workspace : local.meta?.cwd,
      modelLabel: modelLabel || connectingLabel,
      commandCatalog: remote?.commands,
      imageInputEnabled: !remoteActive && local.meta?.imageInputEnabled !== false,
      imageUnderstandingEnabled: !remoteActive && local.meta?.visionFallbackEnabled === true,
      attachmentInputEnabled: !remoteActive,
      pinnedFiles: remote ? undefined : local.meta?.pinnedFiles,
      turnId: remote ? undefined : local.activeTurnId,
      effort: remote ? remote.effort : local.effort,
      localDurableGuidance: !remoteActive,
      context: runtime.context, turnCost: runtime.turnCost, turnRateBand: runtime.turnRateBand,
      currency: runtime.sessionCurrency, cacheHitTokens: runtime.usage?.cacheHitTokens,
      cacheMissTokens: runtime.usage?.cacheMissTokens, balance: runtime.balance,
    },
    context: {
      tabId: remote ? undefined : activeTabId,
      items: runtime.items, context: runtime.context, usage: runtime.usage,
      sessionTokens: runtime.sessionTokens, sessionCost: runtime.sessionCost,
      sessionCurrency: runtime.sessionCurrency, turnTokens: runtime.turnTotalTokens,
      turnCost: runtime.turnCost, turnRateBand: runtime.turnRateBand, balance: runtime.balance,
      sessionGen: runtime.sessionGen, usageSeq: runtime.usageSeq,
    },
    status: {
      context: runtime.context, usage: runtime.usage, balance: runtime.balance,
      running: runtime.running, jobs: runtime.jobs,
      backgroundRuntimes: remote ? [] : backgroundRuntimes,
      sessionTokens: runtime.sessionTokens, turnTokens: runtime.turnTotalTokens,
      lastTurnOutputTokens: runtime.lastTurnOutputTokens, lastTurnModelMs: runtime.lastTurnModelMs,
      lastTurnOutputEstimated: runtime.lastTurnOutputEstimated, lastRequestTps: runtime.lastRequestTps,
      turnCost: runtime.turnCost, turnRateBand: runtime.turnRateBand, cost: runtime.sessionCost,
      currency: runtime.sessionCurrency, modelLabel,
      workspacePath: remote ? tab?.remote?.workspace : local.meta?.workspacePath || local.meta?.workspaceRoot || local.meta?.cwd,
      workspaceName: remote ? tab?.workspaceName : local.meta?.workspaceName,
      gitBranch: remote ? undefined : local.meta?.gitBranch,
    },
  };
}

export function projectConversationLayout(input: {
  chatVisible: boolean; localToolsEnabled: boolean; dockMode: string;
  dockRenderable: boolean; dockGridOpen: boolean; dockOverlay: boolean;
  dockOpen: boolean; dockMaximized: boolean; terminalOpen: boolean;
}) {
  const localDockBlocked = !input.localToolsEnabled && (input.dockMode === "files" || input.dockMode === "changed");
  const dockVisible = input.chatVisible && input.dockRenderable && !localDockBlocked;
  return {
    dockVisible,
    dockGridOpen: input.chatVisible && input.dockGridOpen && !localDockBlocked,
    dockOverlay: dockVisible && input.dockOverlay,
    dockMaximized: input.chatVisible && input.dockOpen && input.dockMaximized,
    terminalOpen: input.chatVisible && input.terminalOpen && input.localToolsEnabled,
  };
}
