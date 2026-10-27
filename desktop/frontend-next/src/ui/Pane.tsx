import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState, useSyncExternalStore } from "react";
import { streamBudget } from "../port/streambudget";
import { loadReceiptPref, receiptPrefOf } from "../state/receiptpref";
import { ReceiptPrefContext } from "../state/foldpref";
import { moneyList } from "../i18n/format";
import { pushToast } from "../state/toasts";
import { reason } from "../i18n/kernel";
import { noteFault } from "./fault";
import { ErrorBar } from "./ErrorBar";
import { t } from "../i18n";
import { hasPendingDecision, posture, runState } from "./decisions";
import { createPortal } from "react-dom";
import type { Checkpoint, ContextBreakdown, JobEntry, McpEntry, SessionStatus, WorkspaceChanges, WorkspaceGit } from "../port/port";
import type { TrajectoryRead } from "../port/wire";
import { chipLabel, initialState, quoteAmount, reduce } from "../state/session";
import type { CurrencyAmount } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { useReplyActions } from "./reply";
import { useGateActions } from "./gates";
import { useQueueActions } from "./queueactions";
import { useRewindActions } from "./rewind";
import { ReloadNotice } from "./RestoreNotice";
import { RewindNotice } from "./RewindNotice";
import { initialTraj, reduceTraj } from "../state/trajectory";
import { Transcript } from "./Transcript";
import { useHistoryPages, useHistorySearch } from "./useHistoryPages";
import { useBackgroundDeltas } from "./deltas";
import { PaneShown } from "./shown";
import { Composer } from "./Composer";
import { draftKey } from "./drafts";
import { Queue, waiting } from "./Queue";
import { SlottedView } from "./SlottedView";
import { key as slotKey } from "./slots";
import { Metrics } from "./Metrics";
import { railOf } from "./panels/derive";
import { accountOf, useHidesAmounts, useWallet } from "./wallet";
import { useOnBump } from "./useOnBump";
import { useShowView } from "./showview";
import { useSubmit } from "./usesubmit";
import { useSurfaceSlots } from "./usesurfaceslots";
import { useSurfaceReplay } from "./surface-replay";
import { MeterRail } from "./MeterRail";
import { useAgentRequest } from "./useAgentRequest";
import { PlanFold } from "./PlanFold";
import { useBroughtResults } from "./useBroughtResults";
import { RunLine } from "./RunLine";
import { AgentCompositionPicker } from "./AgentCompositionPicker";
import type { PaneProps, PaneReport } from "./panetypes";
import { useRate, useTrail } from "./num";
import { useCompactNow } from "./CompactNow";
import { DOCK, Gutter } from "./Gutter";
import { Find } from "./Find";
import { useFind } from "./usefind";
import { RunAnalysis } from "./RunAnalysis";
import { useBrowserTabs } from "./BrowserPanel";
import { useRevealAgentPages, useRevealBrowserOpen } from "./browserreveal";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { useBrowserFull } from "./browserfull";
import { refreshTodos } from "../state/restore";
import { speedOf } from "./speed";
import { RuntimeBar } from "./RuntimeBar";
import { PeerHeldBar } from "./PeerHeldBar";
import { PostureNote, postureNote } from "./PostureNote";
import { SidePanel } from "./SidePanel";
import { cancelJob } from "./jobcancel";
import { ConnectNote } from "./ConnectNote";
import { useStatusPoll } from "./useStatusPoll";
import { useStatusRefresh } from "./useStatusRefresh";
import { useCheckpointRefresh } from "./useCheckpointRefresh";
import { LiveWork, useLiveWork } from "../state/foldpref";
import { loadChangedFilesPref, useShowsChangedFiles } from "../state/changedfiles";
import { loadFoldWorkPref, useFoldsTurnWork } from "../state/foldwork";
import { ChangedFilesPill } from "./ChangedFilesPill";
import { RootMissingBanner } from "./RootMissingBanner";
import { useConversationFiles } from "./useConversationFiles";
import { NewAnswerAnchor } from "./NewAnswerLine";
import { useFrozenNewAnswerAnchor } from "./newAnswerMark";
import { onNewAnswerMarkerChange, sessionKey, showsNewAnswerMarker, useUnreadSince } from "../state/newAnswers";

export type { PaneReport };

// A shared constant, not `?? []`: a fresh empty array every render reads as a
// changed prop to the rail below it.
const NO_JOBS: JobEntry[] = [];
const NO_COST: CurrencyAmount[] = [];

type SessionRead = { kind: "pending" } | { kind: "settled"; status: SessionStatus | null };

const totalsOf = (st: SessionStatus) => ({
  kind: "__totals",
  hit: st.cacheHit,
  miss: st.cacheMiss,
  cost: quoteAmount(st.sessionCostQuote),
  currency: st.sessionCostQuote?.selected?.currency || st.sessionCostQuote?.original.currency,
  coverage: st.sessionCostQuote?.coverage,
  incompleteReason: st.sessionCostQuote?.incompleteReason,
});

function PaneView({ port, hub, rt, title, active, visible, sideHost, side, onFocus, onReport, onSessionChanged, onTurnDone, pulse, findPulse, findTogglePulse = 0, filesPulse = 0, viewRequest, sessionRef, onSettings, onFeedback, onOpenSession, needsProject, onOpenProject, onKeepHere, connect, theme, dockW, dockMax, onDockW, manualBrowser = false, onManualBrowser, alert }: PaneProps) {
  const [s, dispatch] = useReducer(reduce, initialState);
  const [traj, trajDispatch] = useReducer(reduceTraj, initialTraj);
  const [sessionRead, setSessionRead] = useState<SessionRead>({ kind: "pending" });
  const status = sessionRead.kind === "settled" ? sessionRead.status : null;
  const ready = sessionRead.kind === "settled";
  const [tab, showView] = useShowView(rt.id, onManualBrowser);
  const [sideAsk, setSideAsk] = useState(false);
  const [worktreeTarget, setWorktreeTarget] = useState<HTMLDivElement | null>(null);
  const [worktreePhase, setWorktreePhase] = useState<string | null>(null);
  const [mcp, setMcp] = useState<McpEntry[]>([]);
  const [askFocus, setAskFocus] = useState(0);
  const [tree, setTree] = useState<WorkspaceChanges | null>(null);
  const [git, setGit] = useState<WorkspaceGit | null>(null);
  const [ctx, setCtx] = useState<ContextBreakdown | null>(null);
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const pages = useBrowserTabs(port, s.browserTabsMoved);
  const revealBrowser = useCallback(() => onManualBrowser?.(true), [onManualBrowser]);
  useRevealAgentPages(pages, active && !rt.host, revealBrowser);
  useRevealBrowserOpen(s.items, active && !rt.host, revealBrowser);
  const [surfaces, setSurfaces] = useState(0);
  // Analysis owns the whole working canvas. A docked browser and the composer
  // are useful while talking to the agent, but both compete with the timeline
  // for exactly the horizontal/vertical space the analysis view explains.
  const docked = manualBrowser && tab === "flow";
  const workbench = docked || tab === "browser";
  const flow = useRef<HTMLDivElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const running = s.running || !!status?.running; // A paired device can join after turn_started.
  const marker = useSyncExternalStore(onNewAnswerMarkerChange, showsNewAnswerMarker, showsNewAnswerMarker);
  const newAnswerAt = useFrozenNewAnswerAnchor(s.items, useUnreadSince(rt.sessionPath ? sessionKey(rt.host, rt.sessionPath) : undefined), running);
  const fail = useCallback((e: unknown, from?: string) => {
    const shown = reason(e);
    noteFault(e, shown);
    dispatch({ kind: "__error", text: shown, from } as never);
  }, []);
  // What failed beside the work (a setting that did not apply, a link that would
  // not open) goes to the corner; only a refused send or a failed read keeps the bar.
  const notify = useCallback((e: unknown) => {
    const shown = reason(e);
    noteFault(e, shown);
    pushToast(shown);
  }, []);
  const statusRead = useCallback(() => dispatch({ kind: "__error_clear", from: "status" } as never), []);
  const agentRuns = useAgentRequest(port, revealBrowser);
  const openRun = agentRuns.open;
  const conversationFiles = useConversationFiles(port, status?.workspaceRoot ?? rt.root, !!rt.host, visible && tab === "flow", revealBrowser, notify);
  const compact = useCompactNow(port, running || s.items.some((item) => item.t === "compaction" && !item.done), notify);
  // Elapsed is a clock reading and belongs on the tick. Throughput is not: it
  // follows the deltas themselves, and expires rather than being re-derived.
  const tps = useRate(s.outWindow, running);
  const live = useLiveWork(s.items, running);
  // The shape of the last minute, kept while a turn runs. A number alone says
  // how fast it is now; the line says whether it is climbing, stalling or
  // arriving in bursts, which is the question someone watching a run has.
  const trail = useTrail(tps, running);
  // What this turn has actually put on the wire: input counted whether or not
  // the prefix cache took it, and output as it comes back.
  const sent = s.metrics.hit + s.metrics.miss;
  const received = s.metrics.out + s.outLive;
  const speed = useMemo(() => speedOf(traj.rows), [traj.rows]);

  const { pacer, shown } = useBackgroundDeltas(visible, dispatch, trajDispatch);

  const surfaceReplay = useSurfaceReplay(port, dispatch);
  const { load: readHistory, paging: historyPaging } = useHistoryPages(port, dispatch);

  const reloadMcp = useCallback(() => {
    void port.mcp().then((c) => setMcp(c.servers)).catch(() => setMcp([]));
  }, [port]);

  // What the pane already does on mount, reused as the answer to a gap the
  // stream could not close: the transcript is the record, so rebuilding from it
  // is how a hole gets filled rather than rendered as a quiet turn.
  const rebuild = useCallback(() => {
    void surfaceReplay.refresh();
    void readHistory().catch(() => {});
  }, [readHistory, surfaceReplay.refresh]);

  // /status is polled four times a second while a turn runs, and most of those
  // answers are word-for-word the previous one. Swapping in an equal object
  // would repaint the rail and the composer for no news at all.
  const applyStatus = useCallback((next: SessionStatus) => {
    // The typed half /history cannot carry rides every status read: the
    // kernel commits a text-only round before it decides to continue the
    // turn, so only this read closes one or vouches one still runs. A
    // payload that cannot say leaves the wire's own running standing.
    if (next.running !== undefined) dispatch({ kind: "__running", running: next.running });
    setSessionRead((prev) => (prev.kind === "settled" && prev.status && JSON.stringify(prev.status) === JSON.stringify(next)
      ? prev : { kind: "settled", status: next }));
  }, []);

  const readJob = useCallback((id: string) => port.jobOutput(id), [port]);
  const refreshStatus = useStatusRefresh(port, applyStatus, statusRead);

  const [wallet, refreshWallet] = useWallet(port, accountOf(status?.modelRef));
  const hideAmounts = useHidesAmounts();

  useEffect(() => { void loadReceiptPref(port, !rt.host); }, [port, rt.host]);

  const revalue = useCallback(() => {
    port.status().then((st) => { applyStatus(st); dispatch(totalsOf(st) as never); }).catch(() => {});
    refreshWallet();
  }, [port, applyStatus, refreshWallet]);

  // A hole in the stream is the transport's fact; which authority answers it is
  // each model's own. The transcript is rebuilt from /history, the run graph
  // from the read the kernel rebuilds — one gap, two different re-reads.
  useEffect(() => {
    const sub = port.subscribe(
        (ev) => {
          surfaceReplay.observe(ev);
          compact.onEvent(ev);
          pacer.push(ev); if (ev.kind === "turn_done") onTurnDone?.(rt.id);
          // A server finishing its handshake changes what /mcp answers, and this
          // is the only precise signal for it — the turn boundary below is the
          // fallback for changes that arrive without an event.
          if (ev.kind === "mcp_surface_ready" || ev.kind === "extension_status") reloadMcp();
          if (ev.kind === "todo_progress") void refreshTodos(port, dispatch);
          // A turn or prompt moving changes /status. Re-read even for hidden
          // panes: a client may have missed turn_started, and a stale running
          // snapshot otherwise keeps the sidebar live after turn_done.
          if (ev.kind === "turn_started" || ev.kind === "turn_done" || ev.kind === "approval_request" || ev.kind === "ask_request") refreshStatus();
          // Settling one is the other half of the same move, and the receipt
          // rides as a field rather than a kind of its own.
          else if ("decisionReceipt" in ev && ev.decisionReceipt) refreshStatus();
          // The session total is the kernel's, read in the currency it now
          // values in; summing what arrived before the change would mix two.
          if (ev.kind === "notice" && ev.code === "display_currency") revalue();
        },
        () => {
          compact.release();
          pacer.drop();
          rebuild();
          refreshStatus();
        },
      );
    // Only a few panes keep a live stream (see streambudget.ts); this one says
    // whether it is on screen or busy and the budget gives or takes its connection.
    const release = streamBudget.attach(rt.id, sub, refreshStatus, wantRef.current);
    return () => {
      release();
      sub();
    };
  }, [port, pacer, rt.id, onTurnDone, reloadMcp, rebuild, refreshStatus, revalue, compact.onEvent, compact.release, surfaceReplay.observe]);
  const wantRef = useRef({ visible, busy: running });
  wantRef.current = { visible, busy: running };
  useEffect(() => streamBudget.update(rt.id, { visible, busy: running }), [rt.id, visible, running]);

  // What the rows cover is dispatched before the rows themselves, so the table
  // never spends a frame showing a prefix as if it were the whole record.
  const replayTrajectory = useCallback(
    (read: TrajectoryRead) => {
      trajDispatch({ kind: "__coverage", availability: read.availability });
      read.events.forEach((e) => trajDispatch(e));
    },
    [],
  );

  useEffect(() => {
    let alive = true;
    port.trajectory().then((r) => alive && replayTrajectory(r)).catch(() => {});
    // The record and the numbers over it are two reads, not one. /status can go
    // to the network — the provider's wallet endpoint rides it — and pairing the
    // two made the conversation wait on a round trip that has nothing to do with
    // it. Whichever lands first shows what it knows.
    void readHistory(() => alive).catch(() => {});
    port.status().then((st) => {
      if (!alive) return;
      applyStatus(st);
      dispatch(totalsOf(st) as never);
    }).catch(() => {
      if (alive) setSessionRead((prev) => prev.kind === "pending" ? { kind: "settled", status: null } : prev);
    });
    return () => {
      alive = false;
    };
  }, [port, applyStatus, readHistory, replayTrajectory]);

  useEffect(() => {
    if (!running) {
      refreshWallet();
      void refreshTodos(port, dispatch);
    }
  }, [running, refreshWallet, port]);

  useEffect(() => {
    if (pulse) {
      refreshStatus();
      void surfaceReplay.refresh();
    }
  }, [pulse, refreshStatus, surfaceReplay.refresh]);

  // Everything on screen belongs to one session; when the kernel moves this
  // pane to another one — a switch, a new session, a rewind — all of it has to
  // be re-read rather than patched.
  const reloadSession = useCallback((current: () => boolean = () => true) => {
    void surfaceReplay.refresh(current);
    trajDispatch({ kind: "__clear" } as never);
    port.trajectory().then((r) => { if (current()) replayTrajectory(r); }).catch(() => {});
    port.checkpoints().then((cps) => { if (current()) setCheckpoints(cps); }).catch(() => { if (current()) setCheckpoints([]); });
    // Two reads, the same way the first mount takes them: the record does not
    // wait behind the numbers over it.
    const history = readHistory(current);
    port.status().then((st) => {
      if (!current()) return;
      applyStatus(st);
      statusRead();
      dispatch(totalsOf(st) as never);
    }).catch((e) => { if (current()) fail(e, "status"); });
    refreshWallet();
    onSessionChanged();
    return history;
  }, [port, applyStatus, statusRead, refreshWallet, onSessionChanged, replayTrajectory, fail, surfaceReplay.refresh, readHistory]);

  // Both of these read only the user and tool cards, so they key off the
  // revision rather than the items array: a streamed answer leaves every card
  // they look at untouched, and recomputing them per chunk is the whole reason
  // a long session used to slow down. eslint would want `s.items` in the deps;
  // `s.revision` is the narrower truth. Same for the rail's two panels below.
  /* eslint-disable react-hooks/exhaustive-deps */
  const paired = useMemo(() => pairCheckpoints(s.items, checkpoints), [s.revision, checkpoints]);
  const rail = useMemo(() => railOf(s.items, s.executions, s.subagentPhase), [s.revision, s.executions, s.subagentPhase]);

  const jobs = status?.jobs ?? NO_JOBS;
  const counts = useMemo(() => {
    let steps = 0;
    let wrote = 0;
    for (const i of s.items) {
      if (i.t === "tool" && !i.running && !i.tool.readOnly) wrote++;
      if (i.t === "tool") steps++;
    }
    return { steps, wrote };
  }, [s.revision]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // An MCP server connects lazily and fails at first use, so a turn boundary is
  // also when its status can have changed — no timer of its own needed.
  useEffect(() => {
    if (ready) reloadMcp();
  }, [ready, reloadMcp, status?.sessionPath, running]);
  useCheckpointRefresh(port, status?.sessionPath, running, setCheckpoints);
  // A call that may write can have moved the tree before the turn ends.
  const pillOn = useShowsChangedFiles(port);
  const changeCount = tree?.repo ? tree.changes.length : 0;
  const pill = pillOn && !!tree?.repo;
  const [revealChanges, setRevealChanges] = useState(0);
  const openChanges = useCallback(() => {
    setRevealChanges((n) => n + 1);
    showView("browser");
  }, [showView]);
  const [toggleFiles, setToggleFiles] = useState(0);
  // The workbench knows whether its explorer is the open one, so a press on
  // Files is handed to it to decide; a column that is not showing is opened.
  const pressFiles = useCallback(() => {
    if (visible && workbench) setToggleFiles((n) => n + 1);
    else openChanges();
  }, [visible, workbench, openChanges]);
  useOnBump(filesPulse, pressFiles);
  useOnBump(sessionRef?.n ?? 0, () => sessionRef && setInserted((was) => ({ n: was.n + 1, text: "", session: { id: sessionRef.id, title: sessionRef.title } })));
  useOnBump(viewRequest?.n ?? 0, () => {
    if (viewRequest && (viewRequest.to !== "analysis" || traj.rows.length > 0)) showView(viewRequest.to);
  });
  useEffect(() => { void loadChangedFilesPref(port); }, [port]);
  useEffect(() => { void loadFoldWorkPref(port); }, [port]);
  const foldWork = useFoldsTurnWork(port);
  const brought = useBroughtResults(s.items);
  const refreshTree = useCallback((git?: WorkspaceGit) => {
    void port.changes().then(setTree).catch(() => setTree(null));
    if (git) setGit(git);
    else void port.workspaceGit().then(setGit).catch(() => setGit(null));
  }, [port]);
  useEffect(() => { if (ready || counts.wrote) refreshTree(); }, [ready, refreshTree, status?.sessionPath, running, counts.wrote]);

  // One turn can be dozens of model round trips — the session this was measured
  // on ran thirty, from 9k tokens to 57k. Reading the gauge only at the turn
  // boundary froze it for the whole of that, which is exactly when someone
  // watches it. Usage arrives on every round trip, so it is the signal; the
  // kernel keys its own answer on the transcript version, so asking again
  // between trips costs nothing.
  // A fold replaces the history wholesale after its own usage has already been
  // counted, so round trips alone would leave the gauge showing the window from
  // before the compaction — the one moment it moves most.
  const folds = s.items.reduce((n, i) => n + (i.t === "compaction" && i.done ? 1 : 0), 0);
  const roundTrips = s.metrics.hit + s.metrics.miss;
  useEffect(() => {
    if (!ready && !roundTrips && !folds) return;
    port.context().then(setCtx).catch(() => setCtx(null));
  }, [ready, port, roundTrips, folds, status?.sessionPath, running, pulse]);

  // Workspace preparation can move an empty pane before its first turn mints
  // a transcript; the sidebar needs that root as well as later session updates.
  useEffect(() => {
    onSessionChanged();
  }, [status?.workspaceRoot, status?.sessionPath, running, onSessionChanged]);

  // /status is the only source for background jobs and for settings the run does
  // not echo, so a live turn has to re-read it rather than infer from events.
  useStatusPoll(running && visible, refreshStatus);

  const { slots, moveSurface, atComposer, inRail } = useSurfaceSlots(port, s.views, notify);

  const submit = useSubmit({ port, running, dispatch, trajDispatch, refreshStatus, fail });

  const [inserted, setInserted] = useState<{ n: number; text: string; session?: { id: string; title: string } }>({ n: 0, text: "" });
  const { queue, restored, onRestoreText, onQueueEdit, onQueueMove, onQueueRetry, onQueueRefresh, onQueuePause, onQueueRead, onQueueSendNow, onQueueCancel } = useQueueActions({
    port,
    dispatch,
    fail,
    moved: s.queueMoved,
    sessionState: sessionRead.kind,
    sessionPath: status?.sessionPath,
  });
  const { reloadFailure, onReloadSession, restoreNotice, onUndoAvailable, onPrepareRewind, onCommitRewind, onPrepareFileRevert, onCommitFileRevert } = useRewindActions(port, reloadSession, onRestoreText, JSON.stringify([rt.id, rt.root, status?.sessionPath ?? rt.sessionPath]), running);
  const onReadUndo = useCallback(() => port.availableUndo(), [port]);

  const { onApprove, onFullAccess, onPlan, onForget, onExtInvoke, onExtSubmit, onAnswer } = useGateActions({
    port,
    dispatch,
    refreshStatus,
    fail,
    onRevise: () => setAskFocus((n) => n + 1),
  });

  const receiptPref = receiptPrefOf(port);
  const find = useFind(s.items, findPulse, active, useCallback(() => showView("flow"), [showView]), receiptPref, findTogglePulse);
  useHistorySearch(historyPaging, find.open, find.query);

  const onRunDetail = useCallback(() => showView("analysis"), [showView]);
  const previewScope = JSON.stringify([rt.id, rt.root, status?.sessionPath ?? rt.sessionPath]);
  const [previewFile, setPreviewFile] = useState<{ port: typeof port; scope: string; path?: string }>();
  const onActiveFile = useCallback((path: string | undefined) => {
    setPreviewFile((old) => old?.port === port && old.scope === previewScope && old.path === path
      ? old : { port, scope: previewScope, path });
  }, [port, previewScope]);
  const activeFile = visible && workbench && previewFile?.port === port && previewFile.scope === previewScope ? previewFile.path : undefined;
  const { quote, reply, onResend } = useReplyActions({ port, items: s.items, checkpoints, running, model: status?.label, submit: (line) => submit(line).then((r) => r === true), reloadSession, onSettings, onRunDetail, onError: fail, branchName: `${title} · ${t("分支")}`, onBranched: onOpenSession });
  const fileReply = useMemo(() => ({
    ...reply, files: conversationFiles.refs, activeFile,
    onAddFile: (reference: string) => setInserted((was) => ({ n: was.n + 1, text: reference })),
    openDeliveredFile: !rt.host && port.opensFiles() ? (path: string) => port.openDocument(path) : undefined,
    revealDeliveredFile: !rt.host && port.revealsFiles() ? (path: string) => port.revealInFileManager(path) : undefined,
  }), [reply, conversationFiles.refs, rt.host, port, activeFile]);

  // Where the bottom is moves as blocks mount under it, so this only asks the
  // transcript to follow again and lets it scroll itself into place.

  // Who is waited on is the kernel's answer, read from the decision list it
  // already publishes. This used to compare the label on screen against two
  // Chinese literals that the reducer had written — a translation key deciding
  // whether the run reads as moving.
  const blocked = hasPendingDecision(status);
  const browserSize = useBrowserFull(docked, active && visible, blocked);
  const run = runState({ blocked, running, hasItems: s.items.length > 0, terminal: s.terminal });
  const cost = s.metrics.coverage === "none" ? NO_COST : s.metrics.cost;
  const contextPercent = ctx && ctx.window > 0 ? Math.min(100, Math.round((ctx.used / ctx.window) * 100)) : null;
  const walletDisplay = wallet.kind === "read" ? wallet.reading.display : "";

  // A pane out of sight that is asking whether to trust its folder is waiting on
  // the person, though the kernel counts no decision for it; the one in front
  // shows the card itself.
  const trustWaiting = !active && postureNote(status) === "trust";

  // The chrome reads the focused pane. Reporting from an effect keeps it out of
  // render, where it would set state on the parent mid-paint.
  useEffect(() => {
    onReport(rt.id, {
      status,
      title,
      steer: waiting(queue).filter((it) => it.intent === "steer").length,
      run: trustWaiting && run === "idle" ? "halt" : run,
      live: running || blocked || trustWaiting,
      cost: moneyList(cost),
      contextPercent,
      context: ctx,
      mcp,
      wallet: walletDisplay,
      interactions:s.interactions,
      view: docked ? "browser" : tab,
      rows: traj.rows.length,
      surfaces,
    });
  }, [rt.id, onReport, status, title, queue, run, running, blocked, trustWaiting, cost, contextPercent, ctx, mcp, walletDisplay, s.interactions, docked, tab, traj.rows.length, surfaces]);

  return (
    <section
      className="pane"
      data-browser-size={browserSize.size}
      data-run={run}
      data-plan={tab === "flow" && (s.plan.length > 0 || status?.goal) ? "" : undefined}
      data-off={visible ? undefined : ""}
      aria-hidden={visible ? undefined : true}
      data-active={active ? "" : undefined}
      aria-label={title}
      onMouseDownCapture={active ? undefined : onFocus}
      onFocusCapture={active ? undefined : onFocus}
    >
      <Find find={find} pending={historyPaging.hasMore || historyPaging.busy || historyPaging.failed} failed={historyPaging.failed} />

      <div className="pbody rewind-notice-host" ref={body} data-dock={tab === "flow" ? "" : undefined} data-full={tab === "browser" ? "" : undefined}>
      <RewindNotice key={JSON.stringify([rt.id, rt.root, status?.sessionPath ?? rt.sessionPath])} readUndo={onReadUndo} onUndo={onUndoAvailable} busy={!!restoreNotice?.working || !!reloadFailure?.working} refreshKey={checkpoints} moved={s.rewindMoved} running={running} shown={visible && tab === "flow" && !rt.readOnly} />
      <div className="pviews">
      <div className="worktree-stage-host" ref={setWorktreeTarget} hidden={tab !== "flow" || !worktreePhase}/>

      <ReceiptPrefContext.Provider value={receiptPref}>
      <PaneShown.Provider value={shown}>
      <LiveWork.Provider value={live}>
      <NewAnswerAnchor.Provider value={marker ? newAnswerAt : null}>
      <Transcript port={port} historyPaging={historyPaging}
        scope={rt.sessionPath}
        onSettings={onSettings}
        brought={brought}
        foldWork={foldWork}
        turnRunning={running}
        reply={fileReply}
        onResend={onResend}
        items={s.items}
        entering={s.entranceOwed}
        onEntered={(ids) => dispatch({ kind: "__entered", ids } as never)}
        revision={s.revision}
        takeovers={s.takeovers}
        waiting={s.waiting}
        scroll={flow}
        hidden={tab !== "flow" || !!worktreePhase}
        focus={null}
        find={find.at}
        query={find.query}
        onApprove={onApprove}
        onFullAccess={onFullAccess}
        onPlan={onPlan}
        onAnswer={onAnswer}
        onForget={onForget}
        onExtInvoke={onExtInvoke}
        onExtSubmit={onExtSubmit}
        checkpoints={paired}
        onPrepareRewind={onPrepareRewind}
        onCommitRewind={onCommitRewind}
        onPrepareFileRevert={onPrepareFileRevert}
        onCommitFileRevert={onCommitFileRevert}
        needsProject={needsProject}
        onOpenProject={onOpenProject}
        onKeepHere={onKeepHere}
      />
      </NewAnswerAnchor.Provider>
      </LiveWork.Provider>
      </PaneShown.Provider>
      </ReceiptPrefContext.Provider>
      {conversationFiles.menuView}

      <div className="scroll" data-pane="analysis" hidden={tab !== "analysis"}>
        {tab === "analysis" && (
          <RunAnalysis
            rows={traj.rows}
            availability={traj.availability}
            onSave={(name, content) => port.saveText(name, content)}
            onBack={() => showView("flow")}
          />
        )}
      </div>
      </div>
      </div>
      {/* The workbench keeps pages and open files mounted across view changes;
          the divider remains reachable when the split column is closed. */}
      {tab === "flow" && browserSize.size === "split" && (
        <Gutter edge="r" span={DOCK} width={dockW} max={dockMax} label={t("调整浏览器宽度")} open={docked}
          onWidth={onDockW} onOpen={(on) => onManualBrowser?.(on)} />
      )}
      <div className="scroll" data-pane="browser" hidden={tab === "analysis"} inert={!workbench}>
          <WorkbenchPanel
            port={port}
            hub={hub}
            fileRequest={conversationFiles.request}
            agentRequest={agentRuns.request}
            tabs={pages}
            fullMode={docked ? { full: browserSize.size === "full", onToggle: browserSize.toggleFull } : undefined}
            manual={manualBrowser}
            shown={visible && workbench}
            onSurfaces={setSurfaces}
            onActiveFile={onActiveFile}
            scheme={theme === "light" ? "light" : "dark"}
            changes={tree?.changes ?? []} onTreeChanged={refreshTree} revealChanges={revealChanges} toggleFiles={toggleFiles}
            running={running}
            wrote={counts.wrote}
            remote={!!rt.host}
            workspaceRoot={status?.workspaceRoot ?? rt.root}
            onCloseManual={() => onManualBrowser?.(false)}
            onExternal={(url) => void port.openExternal(url).catch(notify)}
            onAddToChat={(reference) => setInserted((was) => ({ n: was.n + 1, text: reference }))}
            onQuoteSelection={reply.onQuoteFile}
          />
      </div>

      {/* Keep the composer mounted so a half-written prompt survives a visit to
          analysis; hidden removes it from layout without throwing its state
          away. */}
      <div className="compose" hidden={tab !== "flow"}>
        <span className="glowring" aria-hidden="true">
          <i />
        </span>
        {/* Everything stacked above the input box shares one ceiling, so no
            child of this region may grow without bound. */}
        <PlanFold plan={s.plan} goal={status?.goal} goalStatus={status?.goalStatus} goalRuntime={status?.goalRuntime} shown={tab === "flow"} paused={blocked} />
        {tab === "flow" && <RunLine label={worktreePhase ? t(["failed", "cleanup_failed"].includes(worktreePhase) ? "创建失败" : worktreePhase === "cancelled" ? "创建已取消" : "准备工作目录") : chipLabel(s, running)} running={running} blocked={blocked} since={s.running ? s.runSince : undefined} sent={sent} received={received} estimated={s.outLive > 0}>
          <AgentCompositionPicker port={port} binding={status?.agentComposition} locked={status?.agentCompositionLocked} running={running} onChanged={refreshStatus} onSettings={() => onSettings("persona")} onError={notify} />
        </RunLine>}
        <div className="composeaux">
          <Queue
            queue={queue}
            running={running}
            onRead={onQueueRead}
            onSendNow={onQueueSendNow}
            onEdit={onQueueEdit}
            onMove={onQueueMove}
            onCancel={onQueueCancel}
            onRetry={onQueueRetry}
            onRefresh={onQueueRefresh}
            onPause={onQueuePause}
          />
          <PeerHeldBar port={port} project={rt.host ? undefined : { path: rt.root, name: rt.name }} />
          {connect && <ConnectNote offer={connect} />}
          <PostureNote port={port} status={status} onChanged={refreshStatus} />
        {/* Views the user (or the extension) put next to the composer. They sit
            above it rather than inside it: the input box is the one thing an
            extension must never be able to crowd out. */}
          {atComposer.length > 0 && (
            <div className="slotrail">
              {atComposer.map((ext) => (
                <SlottedView
                  key={slotKey(ext)}
                  ext={ext}
                  assigned={slots}
                  onAction={(id) => void port.invokeExtensionAction(id).catch(notify)}
                  onMove={(slot) => void moveSurface(ext, slot)}
                />
              ))}
            </div>
          )}
        </div>
        {alert && <div className="cmpalert">{alert}</div>}
        <RootMissingBanner items={s.items} onPick={onOpenProject} />
        {pill && <ChangedFilesPill changes={tree?.changes ?? []} onOpen={openChanges} />}
        <Composer worktreeTarget={worktreeTarget} onWorktreePhase={setWorktreePhase} port={port} fresh={ready && !historyPaging.hasMore && !historyPaging.busy && !historyPaging.failed && !rt.host && s.items.length === 0} status={status} running={running} quote={quote} restore={restored} inject={inserted} focus={askFocus} onSubmit={submit} onChanged={refreshStatus} onError={notify} onSettings={onSettings} side={sideAsk} onSide={() => setSideAsk((on) => !on)} sidePanel={sideAsk ? <SidePanel port={port} onClose={() => setSideAsk(false)} /> : null} changeCount={changeCount} countElsewhere={pill} git={git} onTreeChanged={refreshTree} pulse={pulse} draftKey={draftKey(rt.host ?? "", rt.root, rt.sessionPath || status?.sessionPath || "")} />
        <MeterRail
          tps={tps} trail={trail} running={running} speed={speed} metrics={s.metrics} ctx={ctx} mcp={mcp} compact={compact} cost={cost} turnCost={s.metrics.coverage === "none" ? NO_COST : s.metrics.turn}
          wallet={wallet} hideAmounts={hideAmounts} tasks={rail.tasks} jobs={jobs} onSettings={onSettings}
          onCancelJob={(id) => cancelJob(port, id, notify, refreshStatus).then(() => {})}
          onReadJob={readJob}
          onOpenRun={openRun}
          usage={status?.sessionUsage} quote={status?.sessionCostQuote} tariff={status?.tariff} onTariffFlip={refreshStatus}
        />
        {reloadFailure && <ReloadNotice failure={reloadFailure} onReload={onReloadSession} />}
        {/* Below the box, under a ceiling of their own. Both arrive unbidden and
            both are dismissed one at a time, so nothing else bounds how many can
            be on screen at once. */}
        <div className="composenotes">
          {s.error && (
            <ErrorBar text={s.error} onDismiss={() => dispatch({ kind: "__error", text: "" } as never)} onReport={onFeedback} />
          )}
          <RuntimeBar notices={s.runtime} onSettings={onSettings} onSeen={(id) => dispatch({ kind: "__runtime_seen", id } as never)} watch={s} onStall={(a) => a === "stop" ? void port.cancel().catch(fail) : a === "continue" ? void submit(t("继续")) : dispatch({ kind: a === "mute" ? "__stall_mute" : "__stall_dismiss" } as never)} />
        </div>
      </div>

      {active &&
        side &&
        sideHost &&
        createPortal(
          <Metrics
            port={port}
            metrics={s.metrics}
            tasks={rail.tasks}
            changes={rail.changes}
            stats={rail.stats}
            jobs={status?.jobs ?? NO_JOBS}
            mcp={mcp}
            rate={tps}
            done={!running}
            posture={posture(run, blocked)}
            plan={s.plan}
            blocked={blocked}
            wallet={wallet}
            account={status?.providerDisplayName || accountOf(status?.modelRef)}
            onRefreshWallet={refreshWallet}
            tree={tree}
            ctx={ctx}
            onCtx={setCtx}
            compact={compact}
            yolo={status?.toolApprovalMode === "yolo"}
            onSettings={onSettings}
            panels={s.panels}
            views={inRail}
            onMoveSurface={moveSurface}
            onExtInvoke={onExtInvoke}
          />,
          sideHost,
        )}
    </section>
  );
}

// A frame arriving in one pane must not re-render the others.
export const Pane = memo(PaneView);
