import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useRuntimeSession } from "./useRuntimeState";
import { createLegacyRemotePolicyNoticeTracker } from "./legacyRemotePolicyNotice";
import { app, onRemoteTabEvent, onRemoteTabState } from "./bridge";
import { useRemoteForkTurn } from "./remoteForkTurn";
import { useT } from "./i18n";
import type { CancelOutcome } from "./inboxCancel";
import { initialState, reducer, type ControllerLiveStore, type HistoryLoadOutcome, type HistoryLoadTrigger, type State } from "./useController";
import { TranscriptSessionFollower } from "./transcriptSessionFollower";
import { getTranscriptStore } from "./transcriptStore";
import { isAuthoritativeRemoteStatus, remoteCheckpoints, remoteComposerState, remoteGoalRuntime, remoteGoalView } from "./remoteStatus";
import type { CollaborationMode, CommandInfo, EffortInfo, GoalLifecycleView, GoalRuntime, GoalStatus, QualityFloor, RemoteTabStateValue, TabMeta, ToolApprovalMode, WireEvent } from "./types";
import type { RemoteAskAnswer } from "./remoteTypes";
import type { ForkTargetView } from "./forkTargets";

const loadRemoteSurface = () => import("../components/RemoteSessionSurface");

// The remote session reuses the local transcript pipeline end to end: serve
// frames share the agent event wire form, so they run through the same
// reducer that drives local tabs, and /history hydrates through the same
// history action. The surface and composer therefore consume exactly the
// shapes the local UI consumes.

// RemoteSessionApi is the surface-facing contract of useRemoteSession.
export interface RemoteSessionApi {
  state: RemoteTabStateValue;
  error: string;
  transcript: State;
  liveStore: ControllerLiveStore;
  hydrated: boolean;
  syncMode?: "v2";
  loadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => Promise<HistoryLoadOutcome>;
  loadNewerHistory?: (latest?: boolean) => Promise<HistoryLoadOutcome>;
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
  goalView?: GoalLifecycleView;
  effort?: EffortInfo;
  /** Changes whenever the tab adopts a new/reconnected Serve session snapshot. */
  surfaceGeneration: number;
  promptError: string;
  submit: (text: string, displayText?: string) => Promise<void>;
  runManagementCommand: (text: string, rehydrate?: boolean) => Promise<void>;
  compact: (instructions: string) => Promise<void>;
  cancelTurn: () => Promise<void>;
  approve: (callId: string, decision: string) => Promise<void>;
  resolvePlanDecision: (callId: string, action: "start_execution" | "revise_plan" | "exit_plan", feedback?: string) => Promise<void>;
  answer: (callId: string, answers: RemoteAskAnswer[]) => Promise<void>;
  clearExtensionForm: (pluginId: string, surfaceId: string) => void;
  rewind: (turn: number, scope: string) => Promise<void>;
  /** Creates the child session for one turn; returns its id, or undefined with the reason in promptError. */
  forkTurn: (target: ForkTargetView) => Promise<{ sessionId: string; operationId: string } | undefined>;
  acknowledgeFork: (operationId: string) => Promise<void>;
  setModel: (ref: string) => Promise<void>;
  setEffort: (level: string) => Promise<void>;
  setQualityFloor: (floor: QualityFloor) => Promise<void>;
  pauseGoal: () => Promise<void>;
  resumeGoal: () => Promise<void>;
  editGoal: (objective: string, maxGoalRounds: number | null) => Promise<void>;
  steer: (input: string) => Promise<void>;
  cancelJob: (jobId: string) => Promise<boolean>;
  drainApprovals: (ids: string[]) => void;
  retryHydration: () => Promise<void>;
}

export function useRemoteComposer(
  session: RemoteSessionApi,
  showToast: (message: string, level: "warn" | "error") => void,
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
  showToast: (message: string, level: "warn" | "error") => void,
) {
	const t = useT();
	const active = Boolean(activeTab?.remote);
	const session = useRemoteSession(active && activeTab ? activeTab.id : undefined, activeTab?.remoteState, activeTab?.sessionPath);
	const composer = useRemoteComposer(session, showToast);
	useEffect(() => {
		if (!activeTab?.remote || !activeTab.id) return;
    const legacyQuality = session.transcript.items.some(item => item.kind === "notice" && (item.code === "final_readiness" || item.variant === "delivery"));
		const key = `${activeTab.id}\u0000${activeTab.sessionPath ?? ""}`;
    const notice = legacyRemotePolicyNotice(key, session.composerProfile?.qualityFloor, session.goalRuntime?.stopCause, legacyQuality);
    if (notice) showToast(t(notice), "warn");
	}, [activeTab?.remote, activeTab?.id, activeTab?.sessionPath, session.composerProfile?.qualityFloor, session.transcript.items, session.goalRuntime?.stopCause, showToast, t]);
	return { active, session, ready: active && session.state === "ready" && session.hydrated && Boolean(session.composerProfile), ...composer };
}

const legacyRemotePolicyNotice = createLegacyRemotePolicyNoticeTracker();

export function useRemoteSession(tabId: string | undefined, initial?: RemoteTabStateValue, sessionPath?: string): RemoteSessionApi {
  const runtimeState = useRuntimeSession(tabId, sessionPath);
  const [state, setState] = useState<RemoteTabStateValue>(initial === "disconnected" ? "connecting" : (initial ?? "connecting"));
  const [error, setError] = useState("");
  const transcript = useSyncExternalStore(
    useCallback(listener => tabId ? getTranscriptStore().subscribeState(tabId, listener) : () => {}, [tabId]),
    useCallback(() => (tabId ? getTranscriptStore().states.get(tabId) : undefined) ?? initialState, [tabId]),
  );
  const [modelLabel, setModelLabel] = useState("");
  const [commands, setCommands] = useState<CommandInfo[]>([]);
  const [composerProfile, setComposerProfile] = useState<RemoteSessionApi["composerProfile"]>();
  const [goalRuntime, setGoalRuntime] = useState<GoalRuntime>();
  const [goalView, setGoalView] = useState<GoalLifecycleView>();
  const [effort, setEffortInfo] = useState<EffortInfo>();
  const [surfaceGeneration, setSurfaceGeneration] = useState(0);
  const [promptError, setPromptError] = useState("");
  const [hydrated, setHydrated] = useState(false);
  const olderRef = useRef<((trigger?: HistoryLoadTrigger) => Promise<HistoryLoadOutcome>) | undefined>(undefined);
  const newerRef = useRef<(() => Promise<HistoryLoadOutcome>) | undefined>(undefined);
  const transcriptRef = useRef(transcript);
  const setTranscript = useCallback((update: State | ((state: State) => State)) => {
    const next = typeof update === "function" ? update(transcriptRef.current) : update;
    transcriptRef.current = next;
    if (tabId) getTranscriptStore().setState(tabId, next);
  }, [tabId]);
  const { forkTurn, acknowledgeFork, forkTargetsRefreshRef } = useRemoteForkTurn(app, tabId, sessionPath, setTranscript, setPromptError);
  const liveListenersRef = useRef(new Set<() => void>());
  const hydratedRef = useRef(false);
  const hydratingRef = useRef(false);
  const bufferedEventsRef = useRef<WireEvent[]>([]);
  const hydrateRef = useRef<{ tabId: string; run: (force?: boolean) => Promise<void> } | null>(null);
  const refreshStatusRef = useRef<{ tabId: string; run: () => Promise<void> } | null>(null);
  const reconcileHistoryRef = useRef<(() => Promise<void>) | null>(null);
  const activityRevisionRef = useRef(0);
  const eventTurnIdRef = useRef<string | undefined>(undefined);
  const runtimeAtActivityRef = useRef(runtimeState.state);
  const pendingTurnRef = useRef<{ previousTurnId?: string } | null>(null);

  useEffect(() => {
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
    setGoalView(remoteGoalView(status));
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
    // The store owns mounted content across reconnects and tab switches.
    pendingTurnRef.current = null;
    eventTurnIdRef.current = undefined;
    setModelLabel("");
    setCommands([]);
    setComposerProfile(undefined);
    setGoalRuntime(undefined);
    setGoalView(undefined);
    setEffortInfo(undefined);
    hydratedRef.current = false;
    hydratingRef.current = false;
    bufferedEventsRef.current = [];
    setHydrated(false);
    let cancelled = false;
    let generation = 0;
    let follower: TranscriptSessionFollower | undefined;
    const dispatch = (action: import("./useController").Action) => {
      if (!cancelled) setTranscript(current => reducer(current, action));
    };
    const refreshStatus = async () => {
      const ticket = generation;
      const status = await app.RemoteTabStatus(tabId);
      if (cancelled || ticket !== generation) return;
      applyRemoteStatus(status);
      const { hydrateRemoteTelemetry } = await loadRemoteSurface();
      if (!cancelled && ticket === generation) setTranscript(current => hydrateRemoteTelemetry(current, status));
    };
    const hydrate = async () => {
      const ticket = ++generation;
      follower?.stop();
      follower = new TranscriptSessionFollower(tabId, sessionPath ?? "", true, action => {
        if (!cancelled && ticket === generation) dispatch(action);
      });
      setHydrated(false);
      try {
        await follower.start();
        const { hydrateRemoteTelemetry, loadRemoteStatusSnapshot } = await loadRemoteSurface();
        const loaded = await loadRemoteStatusSnapshot(tabId, mountedState === "ready" ? 3 : 60,
          () => cancelled || ticket !== generation, isAuthoritativeRemoteStatus, true);
        if (!loaded || cancelled || ticket !== generation) return;
        const [snapshot, status] = loaded;
        applyRemoteStatus(status);
        setCommands(Array.isArray(snapshot.commands) ? snapshot.commands as CommandInfo[] : []);
        setTranscript(current => hydrateRemoteTelemetry(reducer(current,
          { type: "checkpoints", checkpoints: remoteCheckpoints(snapshot.checkpoints) }), status));
        hydratedRef.current = true;
        setState("ready");
        setHydrated(true);
        setError("");
        setSurfaceGeneration(value => value + 1);
        void forkTargetsRefreshRef.current?.();
      } catch (error) {
        if (!cancelled && ticket === generation) setError(String(error));
      }
    };
    const offContent = getTranscriptStore().subscribe(tabId, change => dispatch({ type: "history_items_patch", patches: change.patches }));
    olderRef.current = async () => {
      if (transcriptRef.current.historyOlderLoading) return "empty";
      dispatch({ type: "history_older_start" });
      try {
        const page = await getTranscriptStore().loadOlder(tabId, sessionPath ?? "");
        if (!page || cancelled) return "empty";
        if (page.kind === "reload") { await hydrate(); return "loaded"; }
        dispatch({ type: "history_prepend", items: page.prependItems, removeIds: page.removeIds,
          startTurn: page.startTurn, endTurn: page.endTurn, totalTurns: page.totalTurns,
          hasOlder: page.hasOlder, hasNewer: page.hasNewer, revision: page.revision, digest: page.digest });
        return "loaded";
      } catch (error) {
        dispatch({ type: "history_older_error", error: String(error) });
        return "empty";
      }
    };
    hydrateRef.current = { tabId, run: hydrate };
    newerRef.current = async () => {
      if (transcriptRef.current.historyNewerLoading) return "empty";
      dispatch({ type: "history_newer_start" });
      try {
        const page = await getTranscriptStore().loadNewer(tabId, sessionPath ?? "");
        if (!page || cancelled) { dispatch({ type: "history_newer_error", error: "" }); return "empty"; }
        if (page.kind === "stale") { await hydrate(); return "loaded"; }
        dispatch({ type: "history_append", items: page.items,
          startTurn: page.startTurn, endTurn: page.endTurn, totalTurns: page.totalTurns,
          hasOlder: page.hasOlder, hasNewer: page.hasNewer, revision: page.revision, digest: page.digest });
        return "loaded";
      } catch (error) {
        dispatch({ type: "history_newer_error", error: String(error) });
        return "empty";
      }
    };
    refreshStatusRef.current = { tabId, run: refreshStatus };
    reconcileHistoryRef.current = hydrate;
    const offState = onRemoteTabState(tabId, next => {
      if (cancelled) return;
      setState(next.state);
      setError(next.error ?? "");
      if (next.state === "ready") void hydrate();
      else if (next.state === "disconnected") {
        setHydrated(false);
        dispatch({ type: "transcript_connection", status: "disconnected" });
      }
    });
    // The legacy event channel carries ancillary invalidations only.
    const offEvent = onRemoteTabEvent(tabId, raw => {
      const event = raw as WireEvent;
      if (event.kind === "turn_done") {
        void refreshStatus().catch(() => undefined);
        void forkTargetsRefreshRef.current?.();
      }
    });
    if (revivedFromShell) void app.SetActiveTab(tabId).catch(() => undefined);
    void hydrate();
    return () => {
      cancelled = true;
      generation++;
      follower?.stop();
      offContent();
      offState();
      offEvent();
      olderRef.current = undefined;
      newerRef.current = undefined;
      hydrateRef.current = null;
      refreshStatusRef.current = null;
      reconcileHistoryRef.current = null;
    };
  }, [applyRemoteStatus, tabId, sessionPath, setTranscript]);

  const submit = useCallback(async (text: string, displayText = text) => {
    if (!tabId) return;
    if (transcriptRef.current.transcriptProtocol !== 2 || transcriptRef.current.transcriptConnection !== "connected") {
      throw new Error("Transcript v2 is not synchronized. Upgrade Desktop and Serve together, or reconnect.");
    }
    const trimmed = text.trim();
    if (!trimmed) return;
    // Optimistic user bubble, exactly like the local send path. seq rides
    // the reducer's counter; the submission id only needs uniqueness.
    const submissionId = `remote-${Date.now()}`;
    activityRevisionRef.current += 1;
    runtimeAtActivityRef.current = runtimeState.state;
    pendingTurnRef.current = { previousTurnId: runtimeState.state?.turnId };
    setTranscript((s) => reducer(s, { type: "user", text: displayText.trim(), seq: s.seq, submissionId }));
    try {
      if (app.SubmitRemoteTabWithSubmission) await app.SubmitRemoteTabWithSubmission(tabId, trimmed, submissionId);
      else await app.SubmitRemoteTab(tabId, trimmed);
    } catch (e) {
      // Roll the optimistic running flag back — a refused/failed submit must
      // never leave the pill spinning (same contract as the local send path).
      const error = `Send failed: ${e instanceof Error ? e.message : String(e)}`;
      setTranscript((s) => reducer(s, { type: "send_failed", submissionId, error }));
      throw e;
    }
  }, [tabId, runtimeState.state]);

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
        // No fork scope: the serve's /fork switches the parent session; forkTurn creates a child instead.
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
    // Compatibility only. The new client never asks an old server to change
    // policy behind the user's back; its next status remains authoritative.
    if (floor !== "standard" && floor !== "delivery") throw new Error(`Unknown retired execution setting: ${floor}`);
    await app.SetRemoteTabQualityFloor(tabId, floor);
  }, [tabId]);

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

  const editGoal = useCallback(async (objective: string, maxGoalRounds: number | null) => {
    if (!tabId) return;
    await app.EditRemoteTabGoal(tabId, objective, maxGoalRounds);
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
    state, error, transcript, liveStore, hydrated, syncMode: "v2", loadOlderHistory: (_targetTurn?: number, trigger?: HistoryLoadTrigger) => olderRef.current?.(trigger) ?? Promise.resolve("empty"), loadNewerHistory: () => newerRef.current?.() ?? Promise.resolve("empty"), running: transcript.running, modelLabel, commands,
    composerProfile, goalRuntime, goalView, effort, surfaceGeneration, promptError, submit, runManagementCommand, compact, cancelTurn,
    approve, resolvePlanDecision, answer, clearExtensionForm, rewind, forkTurn, acknowledgeFork, setModel, setEffort, setQualityFloor, pauseGoal, resumeGoal, editGoal, steer, cancelJob,
    drainApprovals, retryHydration,
  };
}
