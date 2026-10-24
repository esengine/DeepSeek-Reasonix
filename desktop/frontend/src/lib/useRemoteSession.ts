import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { useRuntimeSession } from "./useRuntimeState";
import { createLegacyRemotePolicyNoticeTracker } from "./legacyRemotePolicyNotice";
import { app, onRemoteTabEvent, onRemoteTabState } from "./bridge";
import { createRemoteHydrationOwner } from "./remoteHydrationOwner";
import { queuedFollowupOutcome } from "./pendingFollowup";
import { onRemoteTabUpdated } from "./remoteTabEvents";
import { hydrateRemoteTelemetry, loadRemoteStatusSnapshot } from "./remoteTelemetry";
import { remoteStatusTakenOver, remoteStatusToAction } from "./remoteStatus";
import { isRemoteTakeoverError } from "./remoteErrors";
import { useRemoteForkTurn } from "./remoteForkTurn";
import { useT } from "./i18n";
import type { CancelOutcome } from "./inboxCancel";
import type { HistoryMessage } from "./types";
import { createTurnSubmissionId, initialState, reducer, type ControllerLiveStore, type HistoryLoadOutcome, type HistoryLoadTrigger, type State } from "./useController";
import { isUnknownSubmissionError } from "./localSubmissionState";
import { TranscriptSessionFollower } from "./transcriptSessionFollower";
import { getTranscriptStore } from "./transcriptStore";
import type { NavigateToTurn } from "./historyTurnNavigation";
import { historyReplaceAction } from "./sessionTranscriptMode";
import { isAuthoritativeRemoteStatus, remoteCheckpoints, remoteComposerState, remoteGoalRuntime, remoteGoalView } from "./remoteStatus";
import type { CollaborationMode, CommandInfo, EffortInfo, GoalLifecycleView, GoalRuntime, GoalStatus, QualityFloor, RemoteTabStateValue, TabMeta, ToolApprovalMode, WireEvent } from "./types";
import type { RemoteAskAnswer } from "./remoteTypes";
import type { ForkTargetView } from "./forkTargets";


// The remote session reuses the local transcript pipeline end to end: serve
// frames share the agent event wire form, so they run through the same
// reducer that drives local tabs, and /history hydrates through the same
// history action. The surface and composer therefore consume exactly the
// shapes the local UI consumes.

// RemoteSessionApi is the surface-facing contract of useRemoteSession.
// Last session path mounted per remote tab. Switching sessions inside one tab
// keeps the tab-keyed reducer state, so a resident paint must know the mounted
// content belongs to a different session before replacing it.
const lastPaintedSessionPathByTab = new Map<string, string>();

export interface RemoteSessionApi {
  state: RemoteTabStateValue;
  error: string;
  transcript: State;
  liveStore: ControllerLiveStore;
  hydrated: boolean;
  /** Content is painted from the resident cache; the authoritative hydrate is still running. */
  revalidating: boolean;
  syncMode?: "v2";
  loadOlderHistory?: (targetTurn?: number, trigger?: HistoryLoadTrigger) => Promise<HistoryLoadOutcome>;
  loadNewerHistory?: (latest?: boolean, current?: () => boolean) => Promise<HistoryLoadOutcome>;
  navigateToTurn?: NavigateToTurn;
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
  submit: (text: string, displayText?: string, choice?: import("./modelApplication").ModelApplicationChoice) => Promise<void>;
  runManagementCommand: (text: string, rehydrate?: boolean) => Promise<void>;
  compact: (instructions: string) => Promise<void>;
  cancelTurn: () => Promise<void>;
  approve: (callId: string, decision: string) => Promise<void>;
  resolvePlanDecision: (callId: string, action: "start_execution" | "revise_plan" | "exit_plan", feedback?: string) => Promise<void>;
  answer: (callId: string, answers: RemoteAskAnswer[]) => Promise<void>;
  clearExtensionForm: (pluginId: string, surfaceId: string, formInstanceId?: string) => void;
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
  const onSend = useCallback(async (displayText: string, submitText = displayText, choice?: import("./modelApplication").ModelApplicationChoice) => {
    const text = (submitText || displayText).trim();
    if (!text) return;
    await session.submit(text, displayText, choice);
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
  // A resident-cache paint shows the session instantly while the authoritative
  // hydrate reconciles in the background.
  const [revalidating, setRevalidating] = useState(false);
  const cachePaintedRef = useRef(false);
  const olderRef = useRef<((trigger?: HistoryLoadTrigger) => Promise<HistoryLoadOutcome>) | undefined>(undefined);
  const navigateRef = useRef<NavigateToTurn | undefined>(undefined);
  const newerRef = useRef<((latest?: boolean, current?: () => boolean) => Promise<HistoryLoadOutcome>) | undefined>(undefined);
  const transcriptRef = useRef(transcript);
  const submitBindingRef = useRef<object>({});
  const setTranscript = useCallback((update: State | ((state: State) => State)) => {
    const owned = (tabId ? getTranscriptStore().states.get(tabId) : undefined) ?? initialState;
    const next = typeof update === "function" ? update(owned) : update;
    transcriptRef.current = next;
    if (tabId) getTranscriptStore().setState(tabId, next);
  }, [tabId]);
  const { forkTurn, acknowledgeFork, forkTargetsRefreshRef } = useRemoteForkTurn(app, tabId, sessionPath, setTranscript, setPromptError);
  const liveListenersRef = useRef(new Set<() => void>());
  const hydratedRef = useRef(false);
  const hydratingRef = useRef(false);
  const bufferedEventsRef = useRef<WireEvent[]>([]);
  // The pre-activation history prime and the follower are never both allowed
  // to write the resident store. "retired" is terminal for the mounted
  // identity: it is set the moment the follower publishes its first cut (or
  // the legacy fallback installs), after which every prime attempt is a no-op.
  const primeRef = useRef<"idle" | "loading" | "primed" | "retired">("idle");
  const hydrateRef = useRef<{ tabId: string; run: (force?: boolean) => Promise<void> } | null>(null);
  const refreshStatusRef = useRef<{ tabId: string; run: () => Promise<void> } | null>(null);
  const reconcileHistoryRef = useRef<(() => Promise<void>) | null>(null);
  const activityRevisionRef = useRef(0);
  const eventTurnIdRef = useRef<string | undefined>(undefined);
  const runtimeAtActivityRef = useRef(runtimeState.state);
  // True while the serve reports a local runtime on the host owns this
  // session; a spectator surface idles with no other status polling, so this
  // flag also drives a slow reconcile loop below. The ref mirrors the last
  // observation so the owner-return transition can be detected at the source.
  const [spectator, setSpectator] = useState(false);
  const spectatorRef = useRef(false);
  const noteOwnership = useCallback((takenOver: boolean) => {
    const wasSpectator = spectatorRef.current;
    spectatorRef.current = takenOver;
    setSpectator(takenOver);
    // Ownership returning is the only path back to the live transcript: the
    // legacy fallback installed a protocol-1 view whose submit() refuses to
    // send, and neither the state channel nor the status poll re-attaches the
    // follower on its own.
    if (wasSpectator && !takenOver) void hydrateRef.current?.run().catch(() => undefined);
  }, []);

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
    getRateOutputQuarters(requestedTabId) {
      return requestedTabId === tabId ? transcriptRef.current.turnRateSample?.outputQuarters : undefined;
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
    noteOwnership(remoteStatusTakenOver(status));
  }, [noteOwnership]);

  useEffect(() => {
    if (!tabId) return;
    transcriptRef.current = getTranscriptStore().states.get(tabId) ?? initialState;
    submitBindingRef.current = {};
    // Restored shells arrive as disconnected shells. Activation must kick the
    // backend revive (SetActiveTab → bootstrap) and never park the UI on a
    // reconnect placeholder — treat them as connecting until ready/error.
    const revivedFromShell = initial === "disconnected";
    const mountedState = revivedFromShell ? "connecting" : (initial ?? "connecting");
    setState(mountedState);
    setError("");
    setPromptError("");
    // The store owns mounted content across reconnects and tab switches.
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
    primeRef.current = "idle";
    spectatorRef.current = false;
    setSpectator(false);
    setHydrated(false);
    cachePaintedRef.current = false;
    setRevalidating(false);
    let cancelled = false;
    let generation = 0;
    let follower: TranscriptSessionFollower | undefined;
    const dispatch = (action: import("./useController").Action) => {
      if (!cancelled) setTranscript(current => reducer(current, action));
    };
    // History-first: the persisted canonical window is readable before the
    // serve activates the runtime, so publish a one-shot durable baseline as
    // soon as the tab's identity resolves. This is deliberately not a second
    // live transcript owner — once the runtime is ready, hydrate()'s follower
    // installs the authoritative protocol-v2 cut and owns everything after it.
    // Mirrors the local primeReadableHistoryForTab contract.
    const transcriptHasContent = () => {
      const mounted = transcriptRef.current;
      return mounted.items.length > 0 || Boolean(mounted.live?.text || mounted.live?.reasoning);
    };
    // Local sessions paint a resident session synchronously before any I/O
    // (useController's peek path). The remote surface had no such path: every
    // switch paid the network round trips even for a session whose transcript
    // was already resident. Paint it, then let hydrate() reconcile.
    const paintResidentCache = () => {
      if (!tabId || !sessionPath) return false;
      const previousPath = lastPaintedSessionPathByTab.get(tabId);
      lastPaintedSessionPathByTab.set(tabId, sessionPath);
      // Same session (reconnect) or a restored mount already showing content:
      // the mounted state is this session's and may be fresher than the cut.
      if (previousPath === sessionPath || (previousPath === undefined && transcriptHasContent())) return false;
      const resident = getTranscriptStore().peek(tabId, sessionPath);
      if (!resident) return false;
      cachePaintedRef.current = true;
      setTranscript(current => reducer(current, historyReplaceAction(resident)));
      hydratedRef.current = true;
      setHydrated(true);
      setRevalidating(true);
      return true;
    };
    const primeEarlyHistory = async () => {
      if (primeRef.current !== "idle") return;
      // Only a blank transcript may be primed, and the check has to run before
      // loadLatest: that call bumps the resident session generation (retiring
      // the follower's in-flight reads) and replaces records before it reads
      // `current`, so a transcript that already has content must never reach
      // it. history_replace has no revision guard either — a stale or empty
      // window landing after the follower's cut would wipe the conversation.
      if (transcriptHasContent()) { primeRef.current = "retired"; return; }
      primeRef.current = "loading";
      // The prime is scoped to this mounted identity and to store ownership,
      // not to a hydrate generation: hydrate() bumps the generation the moment
      // it starts, and a follower that then fails or stalls must not have
      // discarded the only baseline the tab could show.
      const current = () => !cancelled && primeRef.current === "loading";
      try {
        const projection = await getTranscriptStore().loadLatest(tabId, sessionPath ?? "", { current });
        if (!projection || !current()) return;
        if (transcriptHasContent()) { primeRef.current = "retired"; return; }
        primeRef.current = "primed";
        setTranscript(current => reducer(current, historyReplaceAction(projection)));
      } catch {
        // Before the attach handshake lands (or on a legacy serve) the window
        // read is unavailable. A miss stays non-fatal: the next attach
        // publication retries, and the ready-time hydration takes over.
      } finally {
        if (primeRef.current === "loading") primeRef.current = "idle";
      }
    };
    const refreshStatus = async () => {
      const ticket = generation;
      const status = await app.RemoteTabStatus(tabId);
      if (cancelled || ticket !== generation) return;
      applyRemoteStatus(status);
      setTranscript(current => hydrateRemoteTelemetry(current, status));
    };
    const hydration = createRemoteHydrationOwner(async () => {
      const ticket = ++generation;
      follower?.stop();
      follower = new TranscriptSessionFollower(tabId, sessionPath ?? "", true, action => {
        if (cancelled || ticket !== generation) return;
        // The follower's install cut makes it the store owner (connection
        // status frames precede it and own nothing); an early history prime
        // still in flight must not land after that cut.
        if (action.type === "transcript_v2_snapshot") primeRef.current = "retired";
        dispatch(action);
      });
      if (!cachePaintedRef.current) setHydrated(false);
      try {
        await follower.start();
        const loaded = await loadRemoteStatusSnapshot(tabId, mountedState === "ready" ? 3 : 60,
          () => cancelled || ticket !== generation, isAuthoritativeRemoteStatus, true);
        if (!loaded || cancelled || ticket !== generation) return false;
        const [snapshot, status] = loaded;
        applyRemoteStatus(status);
        setCommands(Array.isArray(snapshot.commands) ? snapshot.commands as CommandInfo[] : []);
        setTranscript(current => hydrateRemoteTelemetry(reducer(current,
          { type: "checkpoints", checkpoints: remoteCheckpoints(snapshot.checkpoints) }), status));
        hydratedRef.current = true;
        setState("ready");
        setHydrated(true);
        setError("");
        cachePaintedRef.current = false;
        setRevalidating(false);
        setSurfaceGeneration(value => value + 1);
        void forkTargetsRefreshRef.current?.();
        return true;
      } catch (error) {
        if (cancelled || ticket !== generation) return false;
        // The transcript protocol requires the live runtime that owns the
        // session. Only a session taken over by a local runtime on the serve
        // host (the Follow request answers 409, or status reports the
        // take-over) may fall back to the identity/legacy history view; every
        // other failure keeps its error and waits for the next ready
        // publication or an explicit retry.
        const takenOver = isRemoteTakeoverError(error) || await app.RemoteTabStatus(tabId).then(remoteStatusTakenOver, () => false);
        if (cancelled || ticket !== generation) return false;
        if (!takenOver) { setError(String(error)); return false; }
        try {
          const legacyLoaded = await loadRemoteStatusSnapshot(tabId, mountedState === "ready" ? 3 : 60,
            () => cancelled || ticket !== generation, isAuthoritativeRemoteStatus, false);
          if (!legacyLoaded || cancelled || ticket !== generation) return false;
          const [snapshot, status] = legacyLoaded;
          const messages = Array.isArray(snapshot.history) ? snapshot.history as HistoryMessage[] : [];
          const checkpoints = remoteCheckpoints(snapshot.checkpoints);
          primeRef.current = "retired";
          applyRemoteStatus(status);
          setCommands(Array.isArray(snapshot.commands) ? snapshot.commands as CommandInfo[] : []);
          setTranscript(current => {
            let next = reducer(current, { type: "history", messages, remote: true });
            next = reducer(next, { type: "checkpoints", checkpoints });
            next = reducer(next, remoteStatusToAction(status, Date.now(), next.running));
            return hydrateRemoteTelemetry(next, status);
          });
          hydratedRef.current = true;
          setState("ready");
          setHydrated(true);
          setError("");
          cachePaintedRef.current = false;
          setRevalidating(false);
          setSurfaceGeneration(value => value + 1);
          void forkTargetsRefreshRef.current?.();
          return true;
        } catch (fallbackError) {
          if (!cancelled && ticket === generation) setError(String(fallbackError));
        }
      }
      return false;
    });
    const hydrate = () => hydration.load();
    const offContent = getTranscriptStore().subscribe(tabId, change => change.projection
      ? dispatch({ type: "transcript_records", projection: change.projection, confirmedUsers: [] })
      : dispatch({ type: "history_items_patch", patches: change.patches, expected: change.expected }));
    navigateRef.current = async (target, current) => (await import("./historyTurnNavigation")).navigateHistoryTurn(tabId, transcriptRef.current, () => cancelled ? undefined : transcriptRef.current, dispatch, target, () => !cancelled && current());
    olderRef.current = async () => {
      if (transcriptRef.current.historyOlderLoading) return "empty";
      dispatch({ type: "history_older_start" });
      try {
        const page = await getTranscriptStore().loadOlder(tabId, sessionPath ?? "");
        if (!page || cancelled) return "empty";
        if (page.kind === "reload") { await hydrate(); return "loaded"; }
        if (transcriptRef.current.transcriptProtocol === 2) dispatch({ type: "transcript_records", projection: page, confirmedUsers: [] });
        else dispatch({ type: "history_prepend", items: page.prependItems, removeIds: page.removeIds,
          startTurn: page.startTurn, endTurn: page.endTurn, totalTurns: page.totalTurns,
          hasOlder: page.hasOlder, hasNewer: page.hasNewer, revision: page.revision, digest: page.digest });
        return "loaded";
      } catch (error) {
        dispatch({ type: "history_older_error", error: String(error) });
        return "empty";
      }
    };
    hydrateRef.current = { tabId, run: hydrate };
    newerRef.current = async (latest = false, current = () => true) => {
      if (transcriptRef.current.historyNewerLoading) return "empty";
      dispatch({ type: "history_newer_start" });
      try {
        if (latest) {
          const projection = await getTranscriptStore().loadLatest(tabId, sessionPath ?? "", { current: () => !cancelled && current() });
          if (!projection || cancelled) { dispatch({ type: "history_newer_error", error: "" }); return "empty"; }
          dispatch({ type: "transcript_records", projection: { ...projection, removeIds: [] }, confirmedUsers: [] });
          return "loaded";
        }
        const page = await getTranscriptStore().loadNewer(tabId, sessionPath ?? "");
        if (!page || cancelled) { dispatch({ type: "history_newer_error", error: "" }); return "empty"; }
        if (page.kind === "stale") { await hydrate(); return "loaded"; }
        if (transcriptRef.current.transcriptProtocol === 2) dispatch({ type: "transcript_records", projection: page, confirmedUsers: [] });
        else dispatch({ type: "history_append", items: page.items,
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
    const acceptState: Parameters<typeof onRemoteTabState>[1] = next => {
      if (cancelled || !hydration.observe(next.hydrationRevision)) return;
      setState(next.state);
      setError(next.error ?? "");
      if (next.state === "ready") void hydration.load(next.hydrationRevision);
      else {
        generation++;
        follower?.stop();
        setHydrated(false);
        setRevalidating(false);
        dispatch({ type: "transcript_connection", status: "disconnected" });
      }
    };
    const offState = onRemoteTabState(tabId, acceptState);
    // Ownership flips arrive as tab meta updates (an explicit reclaim clears
    // the pin there before any status poll runs); mirror them into the
    // spectator flag that drives the reconcile loop below.
    const offMeta = onRemoteTabUpdated(meta => {
      if (cancelled || meta?.id !== tabId) return;
      noteOwnership(Boolean(meta.takenOver));
      // The attach publication is the reliable "identity live, activation
      // still in flight" signal — retry the early history read there.
      void primeEarlyHistory();
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
    paintResidentCache();
    void primeEarlyHistory();
    const initialGeneration = generation;
    void Promise.resolve().then(() => app.RemoteTabHydrationState?.(tabId)).catch(() => undefined).then(next => {
      if (cancelled || generation !== initialGeneration) return;
      if (next) acceptState(next);
      else void hydrate(); // Older hosts do not publish hydration revisions.
    });
    return () => {
      submitBindingRef.current = {};
      cancelled = true;
      generation++;
      follower?.stop();
      offContent();
      offState();
      offMeta();
      offEvent();
      olderRef.current = undefined;
      navigateRef.current = undefined;
      newerRef.current = undefined;
      hydrateRef.current = null;
      refreshStatusRef.current = null;
      reconcileHistoryRef.current = null;
    };
  }, [applyRemoteStatus, noteOwnership, tabId, sessionPath, setTranscript]);

  // A spectator surface idles with no status traffic: the running watchdog
  // only reconciles turns, and the read-only composer blocks the sends that
  // would otherwise refresh status. A stale ownership observation could pin
  // the takeover banner forever, so poll at a slow cadence until the serve
  // reports the session free again.
  useEffect(() => {
    if (!tabId || state !== "ready" || !spectator) return;
    const timer = window.setInterval(() => {
      void refreshStatusRef.current?.run().catch(() => undefined);
    }, 5_000);
    return () => window.clearInterval(timer);
  }, [tabId, state, spectator]);

  const submit = useCallback(async (text: string, displayText = text, choice?: import("./modelApplication").ModelApplicationChoice) => {
    if (!tabId) return;
    if (transcriptRef.current.transcriptProtocol !== 2 || transcriptRef.current.transcriptConnection !== "connected") {
      throw new Error("Transcript v2 is not synchronized. Upgrade Desktop and Serve together, or reconnect.");
    }
    const trimmed = text.trim();
    if (!trimmed) return;
    // Optimistic user bubble, exactly like the local send path. seq rides
    // the reducer's counter; the submission id only needs uniqueness.
    const before = getTranscriptStore().states.get(tabId) ?? initialState;
    const binding = submitBindingRef.current;
    const current = () => submitBindingRef.current === binding && getTranscriptStore().states.get(tabId)?.sessionGen === before.sessionGen;
    const submissions = Object.values(before.localSubmissions);
    if (submissions.some(item => item.status === "sending" && (item.submitText ?? item.text).trim() === trimmed)) return;
    const unresolved = submissions.find(item => item.status === "unknown");
    if(unresolved && (choice || (unresolved.submitText ?? unresolved.text).trim()!==trimmed)) {
      throw Object.assign(new Error("Confirm the previous submission before sending another message"),{data:{submissionOutcome:"unknown"}});
    }
    const submissionId = unresolved?.submissionId ?? createTurnSubmissionId(tabId, before.sessionGen, before.seq, before.meta?.runtime?.epoch);
    activityRevisionRef.current += 1;
    runtimeAtActivityRef.current = runtimeState.state;
    if(!unresolved) setTranscript((s) => reducer(s, { type: "user", text: displayText.trim(), submitText:trimmed, seq: s.seq, submissionId }));
    try {
      if (choice) {
        if (!app.SubmitRemoteTabWithModelApplication) throw new Error("Upgrade Desktop to use model application recovery");
        await app.SubmitRemoteTabWithModelApplication(tabId, trimmed, submissionId, choice);
      } else if (app.SubmitRemoteTabWithSubmission) await app.SubmitRemoteTabWithSubmission(tabId, trimmed, submissionId);
      else await app.SubmitRemoteTab(tabId, trimmed);
      if (current()) setTranscript(s => reducer(s, { type: "send_confirmed", submissionId }));
    } catch (e) {
      // A busy-window submit is durably queued by the desktop. The queue strip
      // is its representation, so retract the optimistic bubble instead of
      // reporting a failure; the receipt rides the error for the composer.
      if (queuedFollowupOutcome(e) !== undefined) {
        if (current()) setTranscript((s) => reducer(s, { type: "send_queued", submissionId }));
        throw e;
      }
      // Roll the optimistic running flag back — a refused/failed submit must
      // never leave the pill spinning (same contract as the local send path).
      const error = `Send failed: ${e instanceof Error ? e.message : String(e)}`;
      if (current()) setTranscript((s) => reducer(s, { type: isUnknownSubmissionError(e) ? "turn_submit_unknown" : "send_failed", submissionId, error }));
      throw e;
    }
  }, [tabId, runtimeState.state, setTranscript]);

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

  const clearExtensionForm = useCallback((pluginId: string, surfaceId: string, formInstanceId?: string) => {
    setTranscript((s) => s.extensionForm?.pluginId === pluginId && s.extensionForm.surfaceId === surfaceId &&
      (!formInstanceId || s.extensionForm.formInstanceId === formInstanceId)
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
    state, error, transcript, liveStore, hydrated, revalidating, syncMode: "v2", navigateToTurn: (target, current) => navigateRef.current?.(target, current) ?? Promise.resolve("cancelled"), loadOlderHistory: (_targetTurn?: number, trigger?: HistoryLoadTrigger) => olderRef.current?.(trigger) ?? Promise.resolve("empty"), loadNewerHistory: (latest = false, current) => newerRef.current?.(latest, current) ?? Promise.resolve("empty"), running: transcript.running, modelLabel, commands,
    composerProfile, goalRuntime, goalView, effort, surfaceGeneration, promptError, submit, runManagementCommand, compact, cancelTurn,
    approve, resolvePlanDecision, answer, clearExtensionForm, rewind, forkTurn, acknowledgeFork, setModel, setEffort, setQualityFloor, pauseGoal, resumeGoal, editGoal, steer, cancelJob,
    drainApprovals, retryHydration,
  };
}
