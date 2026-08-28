import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { app, onRemoteTabEvent, onRemoteTabState } from "./bridge";
import type { CancelOutcome } from "./inboxCancel";
import { initialState, reducer, type ControllerLiveStore, type State } from "./useController";
import type { CheckpointMeta, CollaborationMode, CommandInfo, EffortInfo, GoalRuntime, GoalStatus, HistoryMessage, QualityFloor, RemoteTabStateValue, TabMeta, ToolApprovalMode, WireEvent } from "./types";
import type { RemoteAskAnswer } from "./remoteTypes";

const loadRemoteSurface = () => import("../components/RemoteSessionSurface");

// The remote session reuses the local transcript pipeline end to end: serve
// frames share the agent event wire form, so they run through the same
// reducer that drives local tabs, and /history hydrates through the same
// history action. The surface and composer therefore consume exactly the
// shapes the local UI consumes.

// remoteStatusToAction maps the serve's raw /status payload onto the shared
// backend_status action so the remote surface reuses the local tab's running
// reconciliation (including its staleness guards). The serve reports the
// fields it knows; the rest stay undefined and the reducer keeps prior values.
function remoteStatusToAction(status: unknown, snapshotAt: number) {
  const raw = (status ?? null) as { running?: unknown; pendingPrompt?: unknown; backgroundJobs?: unknown; cancelRequested?: unknown; cancellable?: unknown } | null;
  return {
    type: "backend_status" as const,
    running: raw?.running === true,
    pendingPrompt: raw?.pendingPrompt === undefined ? undefined : raw.pendingPrompt === true,
    backgroundJobs: typeof raw?.backgroundJobs === "number" ? raw.backgroundJobs : undefined,
    cancelRequested: raw?.cancelRequested === undefined ? undefined : raw.cancelRequested === true,
    cancellable: raw?.cancellable === undefined ? (raw?.running === true) : raw.cancellable === true,
    snapshotAt,
  };
}

// RemoteSessionApi is the surface-facing contract of useRemoteSession.
export interface RemoteSessionApi {
  state: RemoteTabStateValue;
  error: string;
  transcript: State;
  liveStore: ControllerLiveStore;
  hydrated: boolean;
  running: boolean;
  /** The serve's label for the active model, for the composer capsule. */
  modelLabel: string;
  commands: CommandInfo[];
  composerProfile?: {
    collaborationMode: CollaborationMode;
    toolApprovalMode: ToolApprovalMode;
    goal: string;
    goalStatus?: GoalStatus;
    qualityFloor: QualityFloor;
  };
  goalRuntime?: GoalRuntime;
  effort?: EffortInfo;
  /** Changes whenever the tab adopts a new/reconnected Serve session snapshot. */
  surfaceGeneration: number;
  promptError: string;
  submit: (text: string) => Promise<void>;
  runManagementCommand: (text: string, rehydrate?: boolean) => Promise<void>;
  compact: (instructions: string) => Promise<void>;
  cancelTurn: () => Promise<void>;
  approve: (callId: string, decision: string) => Promise<void>;
  resolvePlanDecision: (callId: string, action: "start_execution" | "revise_plan" | "exit_plan", feedback?: string) => Promise<void>;
  answer: (callId: string, answers: RemoteAskAnswer[]) => Promise<void>;
  clearExtensionForm: (pluginId: string, surfaceId: string) => void;
  rewind: (turn: number, scope: string) => Promise<void>;
  setModel: (ref: string) => Promise<void>;
  setEffort: (level: string) => Promise<void>;
  setQualityFloor: (floor: QualityFloor) => Promise<void>;
  pauseGoal: () => Promise<void>;
  resumeGoal: () => Promise<void>;
  steer: (input: string) => Promise<void>;
  cancelJob: (jobId: string) => Promise<boolean>;
  drainApprovals: (ids: string[]) => void;
  retryHydration: () => Promise<void>;
}

type RemoteStatus = {
  running?: unknown;
  pendingPrompt?: unknown;
  label?: unknown;
  plan?: unknown;
  toolApprovalMode?: unknown;
  goal?: unknown;
  goalStatus?: unknown;
  effort?: unknown;
  used?: unknown;
  window?: unknown;
  cacheHit?: unknown;
  cacheMiss?: unknown;
  lastUsage?: unknown;
  balance?: unknown;
  sessionCostQuote?: unknown;
  jobs?: unknown;
  qualityFloor?: unknown;
  sessionName?: unknown;
  goalRuntime?: unknown;
};

function isAuthoritativeRemoteStatus(status: unknown): status is RemoteStatus {
  if (!status || typeof status !== "object" || Array.isArray(status)) return false;
  const raw = status as RemoteStatus;
  return typeof raw.plan === "boolean"
    && (raw.toolApprovalMode === "ask" || raw.toolApprovalMode === "auto" || raw.toolApprovalMode === "yolo")
    && typeof raw.goal === "string";
}

function remoteComposerState(status: unknown) {
  const raw = (status ?? null) as RemoteStatus | null;
  const goal = typeof raw?.goal === "string" ? raw.goal.trim() : "";
  const toolApprovalMode: ToolApprovalMode = raw?.toolApprovalMode === "auto" || raw?.toolApprovalMode === "yolo"
    ? raw.toolApprovalMode
    : "ask";
  const rawGoalStatus = raw?.goalStatus;
  const goalStatus: GoalStatus | undefined = rawGoalStatus === "running" || rawGoalStatus === "complete"
    || rawGoalStatus === "blocked" || rawGoalStatus === "stopped" ? rawGoalStatus : undefined;
  const effort = raw?.effort as Partial<EffortInfo> | undefined;
  const qualityFloor: QualityFloor = raw?.qualityFloor === "delivery" ? "delivery" : "standard";
  return {
    modelLabel: typeof raw?.label === "string" ? raw.label : "",
    composerProfile: {
      collaborationMode: goal ? "goal" as const : raw?.plan === true ? "plan" as const : "normal" as const,
      toolApprovalMode,
      goal,
      goalStatus,
      qualityFloor,
    },
    effort: effort && typeof effort.supported === "boolean"
      ? {
          supported: effort.supported,
          current: typeof effort.current === "string" ? effort.current : "auto",
          default: typeof effort.default === "string" ? effort.default : "",
          levels: Array.isArray(effort.levels) ? effort.levels.filter((level): level is string => typeof level === "string") : [],
        }
      : undefined,
  };
}

function remoteGoalRuntime(status: unknown): GoalRuntime | undefined {
  const value = (status as RemoteStatus | null)?.goalRuntime;
  if (!value || typeof value !== "object") return undefined;
  const runtime = value as GoalRuntime;
  return typeof runtime.turnsUsed === "number" && typeof runtime.tokensUsed === "number" ? runtime : undefined;
}

function remoteCheckpoints(value: unknown): CheckpointMeta[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((entry) => {
    const raw = (entry ?? null) as Record<string, unknown> | null;
    if (!raw || typeof raw.turn !== "number" || !Number.isFinite(raw.turn)) return [];
    const files = Array.isArray(raw.files)
      ? raw.files.filter((path): path is string => typeof path === "string")
      : [];
    const numericFileCount = typeof raw.files === "number" ? raw.files : raw.fileCount;
    const fileCount = typeof numericFileCount === "number" && Number.isFinite(numericFileCount)
      ? Math.max(0, numericFileCount)
      : files.length;
    return [{
      turn: raw.turn,
      prompt: typeof raw.prompt === "string" ? raw.prompt : "",
      files,
      fileCount,
      filesTruncated: raw.filesTruncated === true,
      turnFileCount: typeof raw.turnFileCount === "number" ? raw.turnFileCount : undefined,
      time: typeof raw.time === "number" ? raw.time : 0,
      canCode: raw.canCode === true,
      canConversation: raw.canConversation === true,
      coverage: typeof raw.coverage === "string" ? raw.coverage : undefined,
      coverageGaps: Array.isArray(raw.coverageGaps)
        ? raw.coverageGaps.filter((gap): gap is string => typeof gap === "string")
        : undefined,
      expiredFilePayload: raw.expiredFilePayload === true,
      activeWriters: typeof raw.activeWriters === "number" ? raw.activeWriters : undefined,
      legacy: raw.legacy === true,
      canUndoFiles: raw.canUndoFiles === true,
      disabledReason: typeof raw.disabledReason === "string" ? raw.disabledReason : undefined,
    }];
  });
}

export function useRemoteComposer(
  session: RemoteSessionApi,
  showToast: (message: string, level: "error") => void,
) {
  const onSend = useCallback(async (displayText: string, submitText = displayText) => {
    const text = (submitText || displayText).trim();
    if (!text) return;
    try {
      await session.submit(text);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
    }
  }, [session, showToast]);
  const onCancel = useCallback(async (_queuedItemIDs?: string[]): Promise<CancelOutcome> => {
    void session.cancelTurn().catch((error) => {
      showToast(error instanceof Error ? error.message : String(error), "error");
    });
    return { discardedItemIds: [] };
  }, [session, showToast]);
  return { onSend, onCancel };
}

export function useActiveRemoteSession(
  activeTab: TabMeta | undefined,
  showToast: (message: string, level: "error") => void,
) {
  const active = Boolean(activeTab?.remote);
  const session = useRemoteSession(active && activeTab ? activeTab.id : undefined, activeTab?.remoteState);
  const composer = useRemoteComposer(session, showToast);
  return { active, session, ready: active && session.state === "ready" && session.hydrated && Boolean(session.composerProfile), ...composer };
}

export function useRemoteSession(tabId: string | undefined, initial?: RemoteTabStateValue): RemoteSessionApi {
  const [state, setState] = useState<RemoteTabStateValue>(initial === "disconnected" ? "connecting" : (initial ?? "connecting"));
  const [error, setError] = useState("");
  const [transcript, setTranscript] = useState<State>(initialState);
  const [modelLabel, setModelLabel] = useState("");
  const [commands, setCommands] = useState<CommandInfo[]>([]);
  const [composerProfile, setComposerProfile] = useState<RemoteSessionApi["composerProfile"]>();
  const [goalRuntime, setGoalRuntime] = useState<GoalRuntime>();
  const [effort, setEffortInfo] = useState<EffortInfo>();
  const [surfaceGeneration, setSurfaceGeneration] = useState(0);
  const [promptError, setPromptError] = useState("");
  const [hydrated, setHydrated] = useState(false);
  const transcriptRef = useRef(transcript);
  const liveListenersRef = useRef(new Set<() => void>());
  const hydratedRef = useRef(false);
  const hydratingRef = useRef(false);
  const bufferedEventsRef = useRef<WireEvent[]>([]);
  const hydrateRef = useRef<{ tabId: string; run: (force?: boolean) => Promise<void> } | null>(null);
  const refreshStatusRef = useRef<{ tabId: string; run: () => Promise<void> } | null>(null);

  useEffect(() => {
    transcriptRef.current = transcript;
    for (const listener of liveListenersRef.current) listener();
  }, [transcript]);

  const liveStore = useMemo<ControllerLiveStore>(() => ({
    subscribe(requestedTabId, listener) {
      if (!tabId || requestedTabId !== tabId) return () => undefined;
      liveListenersRef.current.add(listener);
      return () => liveListenersRef.current.delete(listener);
    },
    getSnapshot(requestedTabId) {
      return requestedTabId === tabId ? transcriptRef.current.live : undefined;
    },
    getModelActiveAt(requestedTabId) {
      return requestedTabId === tabId ? transcriptRef.current.turnModelActiveAt : undefined;
    },
  }), [tabId]);

  const applyRemoteStatus = useCallback((status: unknown) => {
    if (!isAuthoritativeRemoteStatus(status)) return;
    const next = remoteComposerState(status);
    setModelLabel(next.modelLabel);
    setComposerProfile(next.composerProfile);
    setGoalRuntime(remoteGoalRuntime(status));
    setEffortInfo(next.effort);
  }, []);

  useEffect(() => {
    if (!tabId) return;
    // Restored shells arrive as disconnected shells. Activation must kick the
    // backend revive (SetActiveTab → bootstrap) and never park the UI on a
    // reconnect placeholder — treat them as connecting until ready/error.
    const revivedFromShell = initial === "disconnected";
    const mountedState = revivedFromShell ? "connecting" : (initial ?? "connecting");
    setState(mountedState);
    setError("");
    setPromptError("");
    setTranscript(initialState);
    transcriptRef.current = initialState;
    setModelLabel("");
    setCommands([]);
    setComposerProfile(undefined);
    setGoalRuntime(undefined);
    setEffortInfo(undefined);
    hydratedRef.current = false;
    hydratingRef.current = false;
    bufferedEventsRef.current = [];
    setHydrated(false);
    let cancelled = false;
    let hydratePromise: Promise<void> | null = null;
    let hydrateAfterCurrent = false;
    let historyReconcilePromise: Promise<void> | null = null;
    let historyReconcileAfterCurrent = false;
    let connectionGeneration = 0;
    // Reconcile durable history after a turn settles without advancing
    // surfaceGeneration. Serve's broadcaster is intentionally bounded, so a
    // slow subscriber can miss intermediate tool/text frames even when it
    // receives turn_done (or when the watchdog observes the settled status).
    const reconcileHistory = async () => {
      const requestedGeneration = connectionGeneration;
      if (historyReconcilePromise) {
        historyReconcileAfterCurrent = true;
        return historyReconcilePromise;
      }
      historyReconcilePromise = (async () => {
        const snap = await app.RemoteTabSnapshot(tabId);
        // Ready-to-ready state publications represent /new, /clear, or
        // saved-session adoption just as surely as reconnect publications do.
        // A durable-history read started for the previous session must never
        // replace the newly hydrated transcript.
        if (cancelled || connectionGeneration !== requestedGeneration) return;
        const messages = Array.isArray(snap.history) ? (snap.history as HistoryMessage[]) : [];
        const checkpoints = remoteCheckpoints(snap.checkpoints);
        setCommands(Array.isArray(snap.commands) ? snap.commands as CommandInfo[] : []);
        setTranscript((current) => {
          let next = reducer(current, { type: "history", messages, remote: true });
          next = reducer(next, { type: "checkpoints", checkpoints });
          return next;
        });
      })();
      try {
        await historyReconcilePromise;
      } finally {
        const rerun = historyReconcileAfterCurrent;
        historyReconcileAfterCurrent = false;
        historyReconcilePromise = null;
        if (rerun && !cancelled) void reconcileHistory().catch(() => undefined);
      }
    };

    const replayMissingPrompt = async (status: unknown, promptPresent: boolean, expectedGeneration: number) => {
      if ((status as RemoteStatus | null)?.pendingPrompt !== true || promptPresent) return;
      try {
        const replay = await app.ReplayRemoteTabPrompts(tabId);
        if (cancelled || connectionGeneration !== expectedGeneration || !Array.isArray(replay)) return;
        setTranscript((current) => current.approval || current.ask ? current : replay.reduce(
          (next, event) => reducer(next, { type: "event", e: event as WireEvent, remote: true }),
          current,
        ));
      } catch {
        // A transient tunnel failure is retried by the running-state watchdog.
      }
    };

    // Metadata-only commands and turn completion refresh /status without
    // replacing history or advancing surfaceGeneration. That signal is
    // reserved for actual session adoption/reconnects because Transcript uses
    // it as a reveal/remount boundary.
    const refreshStatus = async () => {
      const expectedConnectionGeneration = connectionGeneration;
      const status = await app.RemoteTabStatus(tabId);
      if (cancelled || connectionGeneration !== expectedConnectionGeneration) return;
      const settledWithPossibleFrameLoss = transcriptRef.current.running
        && (status as RemoteStatus | null)?.running === false;
      const { hydrateRemoteTelemetry } = await loadRemoteSurface();
      if (cancelled || connectionGeneration !== expectedConnectionGeneration) return;
      applyRemoteStatus(status);
      setTranscript((current) => hydrateRemoteTelemetry(
        reducer(current, remoteStatusToAction(status, Date.now())),
        status,
      ));
      await replayMissingPrompt(status, Boolean(transcriptRef.current.approval || transcriptRef.current.ask), expectedConnectionGeneration);
      if (settledWithPossibleFrameLoss) void reconcileHistory().catch(() => undefined);
    };
    refreshStatusRef.current = { tabId, run: refreshStatus };

    // Hydrate from the snapshot; retry through the connecting window so a
    // late backend never leaves the surface empty. A forced run re-syncs
    // after a session reset or a reconnect: the snapshot reflects whatever
    // session the serve now holds.
    const hydrate = (force = false) => {
      if (force) {
        hydratedRef.current = false;
        setHydrated(false);
        // A ready event may arrive while the previous connection generation is
        // still hydrating. That in-flight snapshot must finish and be discarded,
        // then hand off to a fresh snapshot for the ready generation.
        if (hydratePromise) hydrateAfterCurrent = true;
      }
      return hydrateLoop();
    };
    const hydrateLoop = async () => {
      if (hydratePromise) return hydratePromise;
      hydratingRef.current = true;
      hydratePromise = (async () => {
        const expectedConnectionGeneration = connectionGeneration;
        // A tab already reported ready has no connection bootstrap left to wait
        // for, so surface a retry affordance promptly. Connecting tabs retain the
        // longer window for slow remote installs and tunnels.
        try {
          const { hydrateRemoteTelemetry, loadRemoteStatusSnapshot } = await loadRemoteSurface();
          // /status is optional in the aggregate snapshot for non-composer
          // consumers, but the remote composer must not submit with guessed
          // plan/approval/goal settings. Fetch it explicitly if the optional
          // member missed; a failure keeps hydration in the retry loop.
          const loaded = await loadRemoteStatusSnapshot(
            tabId,
            mountedState === "ready" ? 3 : 60,
            () => cancelled || hydratedRef.current,
            isAuthoritativeRemoteStatus,
          );
          if (!loaded || cancelled) return;
          if (connectionGeneration !== expectedConnectionGeneration) {
            hydratingRef.current = false;
            return;
          }
          const [snap, status] = loaded;
          const messages = Array.isArray(snap.history) ? (snap.history as HistoryMessage[]) : [];
          hydratedRef.current = true;
          setState("ready");
          setHydrated(true);
          setError("");
          setSurfaceGeneration((generation) => generation + 1);
          applyRemoteStatus(status);
          const checkpoints = remoteCheckpoints(snap.checkpoints);
          setCommands(Array.isArray(snap.commands) ? snap.commands as CommandInfo[] : []);
          const replay = [
            ...(Array.isArray(snap.pendingEvents) ? snap.pendingEvents : []),
            ...bufferedEventsRef.current,
          ] as WireEvent[];
          bufferedEventsRef.current = [];
          hydratingRef.current = false;
          setTranscript((s) => {
            let next = reducer(s, { type: "history", messages, remote: true });
            next = reducer(next, { type: "checkpoints", checkpoints });
            // Hydrate doubles as the post-reconnect running reconciliation:
            // whatever the serve reports about its current state lands now,
            // not only after the next watchdog tick.
            next = reducer(next, remoteStatusToAction(status, Date.now()));
            next = hydrateRemoteTelemetry(next, status);
            const seenPrompts = new Set<string>();
            for (const event of replay) {
              const promptId = event.kind === "approval_request"
                ? event.approval?.id
                : event.kind === "ask_request" ? event.ask?.id : undefined;
              const promptKey = promptId ? `${event.kind}:${promptId}` : "";
              if (promptKey && seenPrompts.has(promptKey)) continue;
              if (promptKey) seenPrompts.add(promptKey);
              next = reducer(next, { type: "event", e: event, remote: true });
            }
            return next;
          });
          await replayMissingPrompt(status, replay.some((event) => event.kind === "approval_request" || event.kind === "ask_request"), expectedConnectionGeneration);
          if (replay.some((event) => event.kind === "turn_done")) {
            // A status poll losing the revision race is benign; the SSE feed
            // and the running-state watchdog converge the surface anyway.
            void refreshStatus().catch(() => undefined);
            void reconcileHistory().catch(() => undefined);
          }
          return;
        } catch (error) {
          if (!cancelled) setError(String(error));
        }
        hydratingRef.current = false;
        if (bufferedEventsRef.current.length > 0) {
          const buffered = bufferedEventsRef.current;
          bufferedEventsRef.current = [];
          setTranscript((current) => buffered.reduce(
            (next, event) => reducer(next, { type: "event", e: event, remote: true }),
            current,
          ));
        }
      })();
      try {
        await hydratePromise;
      } finally {
        const rerun = hydrateAfterCurrent;
        hydrateAfterCurrent = false;
        hydratePromise = null;
        if (rerun && !cancelled) void hydrateLoop();
      }
    };
    hydrateRef.current = { tabId, run: hydrate };
    void hydrateLoop();

    const offState = onRemoteTabState(tabId, (s) => {
      if (cancelled) return;
      // Every backend state publication advances the surface identity. In
      // particular, session rotations intentionally publish ready -> ready,
      // so fencing only non-ready transitions leaves stale history requests
      // able to overwrite the adopted session.
      connectionGeneration += 1;
      setState(s.state === "disconnected" ? "connecting" : s.state);
      setError(s.error ?? "");
      if (s.state === "disconnected") {
        hydratedRef.current = false;
        setHydrated(false);
        void app.SetActiveTab(tabId).catch(() => undefined);
      }
      if (s.state === "ready") {
        void hydrate(true);
      } else {
        // Leaving ready can only mean the serve connection dropped. A turn
        // that was running is now unobservable — stop the pill instead of
        // spinning forever on a turn_done that can never arrive.
        setTranscript((prev) => (prev.running || prev.turnActive ? reducer(prev, { type: "turn_interrupted" }) : prev));
      }
    });
    // Subscribe before activating a restored shell. SetActiveTab republishes
    // terminal bootstrap states, while the snapshot loop covers a ready event
    // that completed before this surface mounted.
    if (revivedFromShell) {
      void app.SetActiveTab(tabId).catch(() => undefined);
    }
    const offEvent = onRemoteTabEvent(tabId, (raw) => {
      if (cancelled) return;
      const event = (raw ?? {}) as WireEvent;
      if (hydratingRef.current) {
        bufferedEventsRef.current.push(event);
        return;
      }
      setTranscript((s) => reducer(s, { type: "event", e: event, remote: true }));
      if (event.kind === "turn_done") {
        void refreshStatus().catch(() => undefined);
        void reconcileHistory().catch(() => undefined);
      }
    });
    return () => {
      cancelled = true;
      hydratingRef.current = false;
      bufferedEventsRef.current = [];
      if (hydrateRef.current?.run === hydrate) hydrateRef.current = null;
      if (refreshStatusRef.current?.run === refreshStatus) refreshStatusRef.current = null;
      offState();
      offEvent();
    };
  }, [applyRemoteStatus, tabId]);

  // Running-state watchdog: while the pill claims a turn is running, poll the
  // serve's /status and feed it through the shared backend_status reducer.
  // This is the remote twin of the local tab's reconcile loop — a lost
  // turn_done frame (dropped SSE, slow-consumer drop, half-dead tunnel) then
  // clears within one tick instead of spinning forever.
  useEffect(() => {
    if (!tabId || !hydrated || state !== "ready" || !transcript.running) return;
    const reconcile = () => {
      const current = refreshStatusRef.current;
      if (!current || current.tabId !== tabId) return;
      void current.run().catch(() => {
        // Transient; the next tick retries.
      });
    };
    const timer = window.setInterval(reconcile, 30_000);
    return () => {
      window.clearInterval(timer);
    };
  }, [tabId, hydrated, state, transcript.running]);

  const submit = useCallback(async (text: string) => {
    if (!tabId) return;
    const trimmed = text.trim();
    if (!trimmed) return;
    // Optimistic user bubble, exactly like the local send path. seq rides
    // the reducer's counter; the submission id only needs uniqueness.
    const submissionId = `remote-${Date.now()}`;
    setTranscript((s) => reducer(s, { type: "user", text: trimmed, seq: s.seq, submissionId }));
    try {
      await app.SubmitRemoteTab(tabId, trimmed);
    } catch (e) {
      // Roll the optimistic running flag back — a refused/failed submit must
      // never leave the pill spinning (same contract as the local send path).
      const error = `Send failed: ${e instanceof Error ? e.message : String(e)}`;
      setTranscript((s) => reducer(s, { type: "send_failed", submissionId, error }));
      throw e;
    }
  }, [tabId]);

  const runManagementCommand = useCallback(async (text: string, rehydrate = false) => {
    if (!tabId) return;
    const trimmed = text.trim();
    if (!trimmed) return;
    // Management verbs produce notices/state changes rather than a model
    // turn, so do not create the optimistic conversational bubble used by
    // submit(). Refresh the authoritative profile after the command settles.
    await app.SubmitRemoteTab(tabId, trimmed);
    if (rehydrate) {
      const hydration = hydrateRef.current;
      if (hydration?.tabId === tabId) await hydration.run(true);
      return;
    }
    const current = refreshStatusRef.current;
    if (current?.tabId === tabId) await current.run();
  }, [tabId]);

  const cancelTurn = useCallback(async () => {
    if (!tabId) return;
    await app.CancelRemoteTab(tabId);
  }, [tabId]);

  const approve = useCallback(async (callId: string, decision: string) => {
    if (!tabId) return;
    setPromptError("");
    try {
      await app.ApproveRemoteTab(tabId, callId, decision);
      setTranscript((s) => s.approval?.id === callId ? { ...s, approval: undefined } : s);
    } catch (error) {
      setPromptError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [tabId]);

  const resolvePlanDecision = useCallback(async (
    callId: string,
    action: "start_execution" | "revise_plan" | "exit_plan",
    feedback = "",
  ) => {
    if (!tabId) return;
    setPromptError("");
    try {
      await app.ResolveRemoteTabPlanDecision(tabId, callId, action, feedback);
      setTranscript((s) => s.approval?.id === callId ? { ...s, approval: undefined } : s);
    } catch (error) {
      setPromptError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [tabId]);

  const answer = useCallback(async (callId: string, answers: RemoteAskAnswer[]) => {
    if (!tabId) return;
    setPromptError("");
    try {
      await app.AnswerRemoteTab(tabId, callId, answers);
      setTranscript((s) => s.ask?.id === callId ? { ...s, ask: undefined } : s);
    } catch (error) {
      setPromptError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [tabId]);

  const clearExtensionForm = useCallback((pluginId: string, surfaceId: string) => {
    setTranscript((s) => s.extensionForm?.pluginId === pluginId && s.extensionForm.surfaceId === surfaceId
      ? reducer(s, { type: "clearExtensionForm" }) : s);
  }, []);

  const retryHydration = useCallback((): Promise<void> => {
    setError("");
    const current = hydrateRef.current;
    if (!current || current.tabId !== tabId) return Promise.resolve();
    return current.run(true);
  }, [tabId]);

  const compact = useCallback(async (instructions: string) => {
    if (!tabId) return;
    await app.CompactRemoteTab(tabId, instructions);
    await retryHydration();
  }, [retryHydration, tabId]);

  const refreshStatus = useCallback((): Promise<void> => {
    const current = refreshStatusRef.current;
    if (!current || current.tabId !== tabId) return Promise.resolve();
    return current.run();
  }, [tabId]);

  const cancelJob = useCallback(async (jobId: string) => {
    if (!tabId) return false;
    try {
      await app.CancelRemoteTabJobs(tabId, [jobId]);
      await refreshStatus();
      return true;
    } catch (error) {
      setPromptError(String(error));
      return false;
    }
  }, [refreshStatus, tabId]);

  const rewind = useCallback(async (turn: number, scope: string) => {
    if (!tabId) return;
    setPromptError("");
    try {
      switch (scope) {
        case "fork":
          await app.ForkRemoteTab(tabId, turn, "");
          break;
        case "summ-from":
          await app.SummarizeRemoteTab(tabId, turn, "from");
          break;
        case "summ-upto":
          await app.SummarizeRemoteTab(tabId, turn, "upto");
          break;
        case "code":
        case "conversation":
        case "both":
          await app.RewindRemoteTab(tabId, String(turn), scope);
          break;
        default:
          throw new Error(`Unsupported remote rewind scope: ${scope}`);
      }
      await retryHydration();
    } catch (error) {
      setPromptError(error instanceof Error ? error.message : String(error));
      throw error;
    }
  }, [retryHydration, tabId]);

  const setEffort = useCallback(async (level: string) => {
    if (!tabId) return;
    await app.SetRemoteTabEffort(tabId, level);
    await refreshStatus();
  }, [refreshStatus, tabId]);

  const setModel = useCallback(async (ref: string) => {
    if (!tabId) return;
    await app.SetRemoteTabModel(tabId, ref);
    await refreshStatus();
  }, [refreshStatus, tabId]);

  const setQualityFloor = useCallback(async (floor: QualityFloor) => {
    if (!tabId) return;
    await app.SetRemoteTabQualityFloor(tabId, floor);
    await refreshStatus();
  }, [refreshStatus, tabId]);

  const pauseGoal = useCallback(async () => {
    if (!tabId) return;
    await app.PauseRemoteTabGoal(tabId);
    await refreshStatus();
  }, [refreshStatus, tabId]);

  const resumeGoal = useCallback(async () => {
    if (!tabId) return;
    await app.ResumeRemoteTabGoal(tabId);
    await refreshStatus();
  }, [refreshStatus, tabId]);

  const steer = useCallback(async (input: string) => {
    if (!tabId) return;
    await app.SteerRemoteTab(tabId, input);
  }, [tabId]);

  const drainApprovals = useCallback((ids: string[]) => {
    setTranscript((current) => reducer(current, { type: "approval_drained", ids, epoch: current.promptEpoch }));
  }, []);

  return {
    state, error, transcript, liveStore, hydrated, running: transcript.running, modelLabel, commands,
    composerProfile, goalRuntime, effort, surfaceGeneration, promptError, submit, runManagementCommand, compact, cancelTurn,
    approve, resolvePlanDecision, answer, clearExtensionForm, rewind, setModel, setEffort, setQualityFloor, pauseGoal, resumeGoal, steer, cancelJob,
    drainApprovals, retryHydration,
  };
}
