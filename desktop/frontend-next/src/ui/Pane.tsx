import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { money } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import { hasPendingDecision, posture, runState } from "./decisions";
import { createPortal } from "react-dom";
import type { Checkpoint, ContextBreakdown, JobEntry, McpEntry, SessionStatus, WorkspaceChanges } from "../port/port";
import type { TrajectoryRead } from "../port/wire";
import { chipLabel, fromHistory, initialState, quoteAmount, reduce } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { useReplyActions } from "./reply";
import { useGateActions } from "./gates";
import { useQueueActions } from "./queueactions";
import { useRewindActions } from "./rewind";
import { RewindNotice } from "./RewindNotice";
import { initialTraj, reduceTraj } from "../state/trajectory";
import { Transcript } from "./Transcript";
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
import { PaneNav } from "./PaneNav";
import { useShowView } from "./showview";
import { useSubmit } from "./usesubmit";
import { useSurfaceSlots } from "./usesurfaceslots";
import { MeterRail } from "./MeterRail";
import { PlanFold } from "./PlanFold";
import { RunLine } from "./RunLine";
import type { PaneProps, PaneReport } from "./panetypes";
import { useRate, useTrail } from "./num";
import { DOCK, Gutter } from "./Gutter";
import { Find } from "./Find";
import { useFind } from "./usefind";
import { RunAnalysis } from "./RunAnalysis";
import { useBrowserTabs } from "./BrowserPanel";
import { useRevealAgentPages, useRevealBrowserOpen } from "./browserreveal";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { refreshTodos } from "../state/restore";
import { speedOf } from "./speed";
import { RuntimeBar } from "./RuntimeBar";
import { PostureNote } from "./PostureNote";
import { useStatusPoll } from "./useStatusPoll";
import { useCheckpointRefresh } from "./useCheckpointRefresh";
import { LiveWork, useLiveWork } from "../state/foldpref";

export type { PaneReport };

// A shared constant, not `?? []`: a fresh empty array every render reads as a
// changed prop to the rail below it.
const NO_JOBS: JobEntry[] = [];

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

function PaneView({ port, rt, title, active, visible, sideHost, side, onFocus, onReport, onSessionChanged, onTurnDone, pulse, findPulse, onSettings, needsProject, onOpenProject, onKeepHere, theme, dockW, dockMax, onDockW, manualBrowser = false, onManualBrowser, alert }: PaneProps) {
  const [s, dispatch] = useReducer(reduce, initialState);
  const [traj, trajDispatch] = useReducer(reduceTraj, initialTraj);
  const [sessionRead, setSessionRead] = useState<SessionRead>({ kind: "pending" });
  const status = sessionRead.kind === "settled" ? sessionRead.status : null;
  const ready = sessionRead.kind === "settled";
  const [tab, showView] = useShowView(rt.id, onManualBrowser);
  const [pinned, setPinned] = useState(true);
  const [jump, setJump] = useState(0);
  const [mcp, setMcp] = useState<McpEntry[]>([]);
  const [askFocus, setAskFocus] = useState(0);
  const [tree, setTree] = useState<WorkspaceChanges | null>(null);
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
  const running = s.running || !!status?.running; // A paired device can join after turn_started.
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

  const reloadMcp = useCallback(() => {
    void port.mcp().then((c) => setMcp(c.servers)).catch(() => setMcp([]));
  }, [port]);

  // What the pane already does on mount, reused as the answer to a gap the
  // stream could not close: the transcript is the record, so rebuilding from it
  // is how a hole gets filled rather than rendered as a quiet turn.
  const rebuild = useCallback(() => {
    port
      .history()
      .then((msgs) => {
        const restored = fromHistory(msgs);
        dispatch({ kind: "__restore", ...restored });
      })
      .catch(() => {});
  }, [port]);

  // /status is polled four times a second while a turn runs, and most of those
  // answers are word-for-word the previous one. Swapping in an equal object
  // would repaint the rail and the composer for no news at all.
  const applyStatus = useCallback((next: SessionStatus) => {
    setSessionRead((prev) => (prev.kind === "settled" && prev.status && JSON.stringify(prev.status) === JSON.stringify(next)
      ? prev : { kind: "settled", status: next }));
  }, []);

  const refreshStatus = useCallback(() => port.status().then(applyStatus).catch(() => {}), [port, applyStatus]);

  const [wallet, refreshWallet] = useWallet(port, accountOf(status?.modelRef));
  const hideAmounts = useHidesAmounts();

  const revalue = useCallback(() => {
    port.status().then((st) => { applyStatus(st); dispatch(totalsOf(st) as never); }).catch(() => {});
    refreshWallet();
  }, [port, applyStatus, refreshWallet]);

  // A hole in the stream is the transport's fact; which authority answers it is
  // each model's own. The transcript is rebuilt from /history, the run graph
  // from the read the kernel rebuilds — one gap, two different re-reads.
  useEffect(
    () =>
      port.subscribe(
        (ev) => {
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
          pacer.drop();
          rebuild();
          refreshStatus();
        },
      ),
    [port, pacer, rt.id, onTurnDone, reloadMcp, rebuild, refreshStatus, revalue],
  );

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
    port.history().then((msgs) => {
      if (!alive) return;
      const restored = fromHistory(msgs);
      dispatch({ kind: "__restore", ...restored });
    });
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
  }, [port, applyStatus]);

  useEffect(() => {
    if (!running) {
      refreshWallet();
      void refreshTodos(port, dispatch);
    }
  }, [running, refreshWallet, port]);

  useEffect(() => {
    if (pulse) refreshStatus();
  }, [pulse, refreshStatus]);

  const fail = useCallback((e: unknown) => {
    // A refusal carries a code; say() turns it into this window's language.
    // Anything else is an ordinary failure and prints as itself.
    dispatch({ kind: "__error", text: reason(e) } as never);
  }, []);

  // History, checkpoints and status belong to one session and must be reloaded together.
  const reloadSession = useCallback(() => {
    trajDispatch({ kind: "__clear" } as never);
    port.trajectory().then(replayTrajectory).catch(() => {});
    port.checkpoints().then(setCheckpoints).catch(() => setCheckpoints([]));
    // Two reads, the same way the first mount takes them: the record does not
    // wait behind the numbers over it.
    const history = port.history().then((msgs) => {
      const r = fromHistory(msgs);
      dispatch({ kind: "__restore", ...r });
    });
    port.status().then((st) => {
      applyStatus(st);
      dispatch(totalsOf(st) as never);
    });
    refreshWallet();
    onSessionChanged();
    return history;
  }, [port, applyStatus, refreshWallet, onSessionChanged, replayTrajectory]);

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
  const refreshTree = useCallback(() => void port.changes().then(setTree).catch(() => setTree(null)), [port]);
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
  }, [ready, port, roundTrips, folds, status?.sessionPath, running]);

  // The sidebar has to hear about this pane's session twice: when the first
  // turn mints the file (before that there is no row to show) and when the turn
  // ends (that is when it has a generated title and a turn count). Without it a
  // brand-new conversation only appeared in the tree once its pane was closed.
  useEffect(() => {
    onSessionChanged();
  }, [status?.sessionPath, running, onSessionChanged]);

  // /status is the only source for background jobs and for settings the run does
  // not echo, so a live turn has to re-read it rather than infer from events.
  useStatusPoll(running && visible, refreshStatus);

  const { slots, moveSurface, atComposer, inRail } = useSurfaceSlots(port, s.views, fail);

  const submit = useSubmit({ port, running, dispatch, trajDispatch, refreshStatus, fail });

  const { queue, restored, onRestoreText, onQueueEdit, onQueueMove, onQueueRetry, onQueueRefresh, onQueuePause, onQueueRead, onQueueSendNow, onQueueCancel } = useQueueActions({
    port,
    dispatch,
    fail,
    moved: s.queueMoved,
    sessionState: sessionRead.kind,
    sessionPath: status?.sessionPath,
  });
  const { onReadUndo, onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert } = useRewindActions(port, reloadSession, onRestoreText);

  const { onApprove, onFullAccess, onPlan, onForget, onExtInvoke, onExtSubmit, onAnswer } = useGateActions({
    port,
    dispatch,
    refreshStatus,
    fail,
    onRevise: () => setAskFocus((n) => n + 1),
  });

  const find = useFind(s.items, findPulse, active, useCallback(() => showView("flow"), [showView]));

  const onRunDetail = useCallback(() => showView("analysis"), [showView]);
  const { quote, reply, onResend } = useReplyActions({ port, items: s.items, checkpoints, running, model: status?.label, submit, reloadSession, onSettings, onRunDetail, onError: fail });

  // Where the bottom is moves as blocks mount under it, so this only asks the
  // transcript to follow again and lets it scroll itself into place.
  const toLatest = () => setJump((n) => n + 1);

  // Who is waited on is the kernel's answer, read from the decision list it
  // already publishes. This used to compare the label on screen against two
  // Chinese literals that the reducer had written — a translation key deciding
  // whether the run reads as moving.
  const blocked = hasPendingDecision(status);
  const run = runState({ blocked, running, hasItems: s.items.length > 0, terminal: s.terminal });
  const cost = money(s.metrics.cost, s.metrics.currency);
  const contextPercent = ctx && ctx.window > 0 ? Math.min(100, Math.round((ctx.used / ctx.window) * 100)) : null;
  const walletDisplay = wallet.kind === "read" ? wallet.reading.display : "";

  // The chrome reads the focused pane. Reporting from an effect keeps it out of
  // render, where it would set state on the parent mid-paint.
  useEffect(() => {
    onReport(rt.id, {
      status,
      title,
      steer: waiting(queue).filter((it) => it.intent === "steer").length,
      run,
      live: running || blocked,
      cost,
      contextPercent,
      context: ctx,
      mcp,
      wallet: walletDisplay,
    });
  }, [rt.id, onReport, status, title, queue, run, running, blocked, cost, contextPercent, ctx, mcp, walletDisplay]);

  return (
    <section
      className="pane"
      data-run={run}
      data-off={visible ? undefined : ""}
      aria-hidden={visible ? undefined : true}
      data-active={active ? "" : undefined}
      aria-label={title}
      onMouseDownCapture={active ? undefined : onFocus}
      onFocusCapture={active ? undefined : onFocus}
    >
      <PaneNav
        view={docked ? "browser" : tab}
        onPick={showView}
        rows={traj.rows.length}
        surfaces={surfaces}
      />

      <Find find={find} />

      <div className="pbody" data-dock={tab === "flow" ? "" : undefined} data-full={tab === "browser" ? "" : undefined}>
      <RewindNotice key={`${rt.id}:${rt.sessionPath || status?.sessionPath || ""}`} readUndo={onReadUndo} onUndo={onUndoRewind} refreshKey={checkpoints} running={running} shown={visible && tab === "flow" && !rt.readOnly} />
      <div className="pviews">

      <PaneShown.Provider value={shown}>
      <LiveWork.Provider value={live}>
      <Transcript
        reply={reply}
        onResend={onResend}
        items={s.items}
        entering={s.entranceOwed}
        onEntered={(ids) => dispatch({ kind: "__entered", ids } as never)}
        revision={s.revision}
        takeovers={s.takeovers}
        waiting={s.waiting}
        scroll={flow}
        hidden={tab !== "flow"}
        onPinned={setPinned}
        jump={jump}
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
        onReadUndo={onReadUndo} onUndoRewind={onUndoRewind}
        onPrepareFileRevert={onPrepareFileRevert}
        onCommitFileRevert={onCommitFileRevert}
        needsProject={needsProject}
        onOpenProject={onOpenProject}
        onKeepHere={onKeepHere}
      />
      </LiveWork.Provider>
      </PaneShown.Provider>

      <div className="scroll" data-pane="analysis" hidden={tab !== "analysis"}>
        {tab === "analysis" && (
          <RunAnalysis
            rows={traj.rows}
            availability={traj.availability}
            onSave={(name, content) => port.saveText(name, content)}
          />
        )}
      </div>
      </div>
      </div>
      {/* Kept mounted rather than switched on: the open files, the browsers and
          where each had got to are what a glance at the conversation must not
          cost. */}
      {/* Mounted shut as well as open: shut is where it becomes the tab that
          brings the browser back, which is the only affordance left once the
          column has no width. */}
      {tab === "flow" && (
        <Gutter edge="r" span={DOCK} width={dockW} max={dockMax} label={t("调整浏览器宽度")} open={docked}
          onWidth={onDockW} onOpen={(on) => onManualBrowser?.(on)} />
      )}
      <div className="scroll" data-pane="browser" hidden={tab === "analysis"} inert={!workbench}>
          <WorkbenchPanel
            port={port}
            tabs={pages}
            manual={manualBrowser}
            shown={visible && workbench}
            onSurfaces={setSurfaces}
            scheme={theme === "light" ? "light" : "dark"}
            changes={tree?.changes ?? []} onTreeChanged={refreshTree}
            running={running}
            wrote={counts.wrote}
            remote={!!rt.host}
            onCloseManual={() => onManualBrowser?.(false)}
            onExternal={(url) => void port.openExternal(url).catch(fail)}
          />
      </div>

      {/* Keep the composer mounted so a half-written prompt survives a visit to
          analysis; hidden removes it from layout without throwing its state
          away. */}
      <div className="compose" hidden={tab !== "flow"}>
        <button className="jump" hidden={pinned || tab !== "flow"} onClick={toLatest}>
          {t("↓ 回到最新")}
        </button>
        <span className="glowring" aria-hidden="true">
          <i />
        </span>
        {/* Everything stacked above the input box shares one ceiling, so no
            child of this region may grow without bound. */}
        <PlanFold plan={s.plan} shown={tab === "flow"} />
        {tab === "flow" && <RunLine label={chipLabel(s, running)} running={running} blocked={blocked} sent={sent} received={received} estimated={s.outLive > 0} />}
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
                  onAction={(id) => void port.invokeExtensionAction(id).catch(fail)}
                  onMove={(slot) => void moveSurface(ext, slot)}
                />
              ))}
            </div>
          )}
        </div>
        {alert && <div className="cmpalert">{alert}</div>}
        <Composer port={port} status={status} running={running} quote={quote} restore={restored} focus={askFocus} onSubmit={submit} onChanged={refreshStatus} onError={fail} onSettings={onSettings} changeCount={tree?.repo ? tree.changes.length : 0} pulse={pulse} draftKey={draftKey(rt.host ?? "", rt.root, rt.sessionPath || status?.sessionPath || "")} />
        <MeterRail
          tps={tps} trail={trail} running={running} speed={speed} metrics={s.metrics} ctx={ctx} mcp={mcp} cost={cost}
          wallet={wallet} hideAmounts={hideAmounts} tasks={rail.tasks} jobs={jobs} onSettings={onSettings}
          onCancelJob={(id) => port.cancelJob(id).then(refreshStatus, fail)}
        />
        {/* Below the box, under a ceiling of their own. Both arrive unbidden and
            both are dismissed one at a time, so nothing else bounds how many can
            be on screen at once. */}
        <div className="composenotes">
          {s.error && (
            <div className="errbar" role="alert">
              <span>{s.error}</span>
              <button onClick={() => dispatch({ kind: "__error", text: "" } as never)}>{t("知道了")}</button>
            </div>
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
            wallet={wallet}
            account={status?.providerDisplayName || accountOf(status?.modelRef)}
            onRefreshWallet={refreshWallet}
            tree={tree}
            ctx={ctx}
            onCtx={setCtx}
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
