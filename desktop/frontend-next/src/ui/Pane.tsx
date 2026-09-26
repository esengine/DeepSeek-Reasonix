import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { money } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import { hasPendingDecision, posture, runState } from "./decisions";
import { createPortal } from "react-dom";
import { HttpError } from "../port/port";
import type { AgentPort, Checkpoint, ContextBreakdown, JobEntry, McpEntry, SessionStatus, WorkspaceChanges } from "../port/port";
import type { RuntimeView } from "../port/hub";
import type { TrajectoryRead } from "../port/wire";
import { currentStep, fromHistory, initialState, localId, quoteAmount, reduce, stepDone, stepLabel } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { DeckChips, type Deck } from "./DeckChips";
import { Plan } from "./Plan";
import { useReplyActions } from "./reply";
import { useGateActions } from "./gates";
import { useQueueActions } from "./queueactions";
import { RunTokens } from "./RunTokens";
import { useRewindActions } from "./rewind";
import { initialTraj, reduceTraj } from "../state/trajectory";
import { Transcript } from "./Transcript";
import { Composer } from "./Composer";
import { Queue } from "./Queue";
import { SlottedView } from "./SlottedView";
import { key as slotKey, placement } from "./slots";
import { Metrics } from "./Metrics";
import { railOf } from "./panels/derive";
import { ABSENT, accountOf, type Wallet } from "./wallet";
import { swapping } from "./swap";
import { PaneNav, type PaneView } from "./PaneNav";
import { useRate, useTrail } from "./num";
import { Spark } from "./Spark";
import { StudioIcon } from "./StudioIcon";
import { useDismiss } from "./dismiss";
import { ContextSummaryCard } from "./ContextSummaryCard";
import { DOCK, Gutter } from "./Gutter";
import { Find } from "./Find";
import { useFind } from "./usefind";
import { RunAnalysis } from "./RunAnalysis";
import { useBrowserTabs } from "./BrowserPanel";
import { useRevealAgentPages, useRevealBrowserOpen } from "./browserreveal";
import { WorkbenchPanel } from "./WorkbenchPanel";
import { refreshTodos } from "../state/restore";
import { RMark } from "./RMark";
import { speedOf } from "./speed";
import { RuntimeBar } from "./RuntimeBar";
import { LiveWork, useLiveWork } from "../state/foldpref";


// PaneReport is what the window's own chrome needs from whichever pane has
// focus: everything else about a session stays inside the pane that owns it.
export interface PaneReport {
  status: SessionStatus | null;
  title: string;
  steer: number;
  run: string;
  // Whether the turn is actually moving or waiting on you. "halt" cannot answer
  // this: a reopened history sits at halt too, and closing that costs nothing.
  live: boolean;
  cost: string;
  contextPercent: number | null;
  context: ContextBreakdown | null;
  mcp: McpEntry[];
  wallet: string;
}

// A shared constant, not `?? []`: a fresh empty array every render reads as a
// changed prop to the rail below it.
const NO_JOBS: JobEntry[] = [];

const totalsOf = (st: SessionStatus) => ({
  kind: "__totals",
  hit: st.cacheHit,
  miss: st.cacheMiss,
  cost: quoteAmount(st.sessionCostQuote),
  coverage: st.sessionCostQuote?.coverage,
  incompleteReason: st.sessionCostQuote?.incompleteReason,
});

interface Props {
  port: AgentPort;
  rt: RuntimeView;
  title: string;
  active: boolean;
  // Where the metrics rail lives. Only the focused pane renders into it, so the
  // column stays on the window's edge instead of appearing between two panes.
  sideHost: HTMLElement | null;
  side: boolean;
  onFocus: () => void;
  // Every pane reports, not just the focused one: a tab has to show that the
  // conversation behind it is still working.
  onReport: (id: string, report: PaneReport) => void;
  // Off-screen panes stay mounted — their stream, transcript and scroll
  // position are exactly what a tab switch must not throw away.
  visible: boolean;
  onSessionChanged: () => void;
  // Bumped when something outside this pane changed a setting that belongs to
  // its session. /status is polled only while a turn runs, so without this the
  // pane keeps reporting the posture it had when it opened.
  pulse: number;
  findPulse: number;
  onSettings: (section?: string) => void;
  // 这个窗口还没有人选过的项目文件夹。空转录是唯一说得出这句话的地方 —— 那里
  // 本来就在替一段还没开始的对话说明它该怎么开始。
  needsProject: boolean;
  onOpenProject: () => void;
  onKeepHere: () => void;
  // A prop, not a document read: this pane is memoised past an attribute flip.
  theme: string;
  // Rides on `.app`: that is where the divider writes while a drag is in flight.
  dockW: number;
  onDockW: (w: number) => void;
  manualBrowser?: boolean;
  onManualBrowser?: (on: boolean) => void;
  // The window's failed-request notice, handed only to the pane in front.
  alert?: ReactNode;
}

function PaneView({ port, rt, title, active, visible, sideHost, side, onFocus, onReport, onSessionChanged, pulse, findPulse, onSettings, needsProject, onOpenProject, onKeepHere, theme, dockW, onDockW, manualBrowser = false, onManualBrowser, alert }: Props) {
  const [s, dispatch] = useReducer(reduce, initialState);
  const [traj, trajDispatch] = useReducer(reduceTraj, initialTraj);
  const [status, setStatus] = useState<SessionStatus | null>(null);
  const [tab, setTab] = useState<PaneView>("flow");
  const [pinned, setPinned] = useState(true);
  const [jump, setJump] = useState(0);
  const [mcp, setMcp] = useState<McpEntry[]>([]);
  const [askFocus, setAskFocus] = useState(0);
  const [tree, setTree] = useState<WorkspaceChanges | null>(null);
  const [ctx, setCtx] = useState<ContextBreakdown | null>(null);
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const [slots, setSlots] = useState<Record<string, string>>({});
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
  const [meterOpen, setMeterOpen] = useState(false);
  const meterRef = useRef<HTMLDivElement>(null);
  const closeMeter = useCallback(() => setMeterOpen(false), []);
  useDismiss(meterOpen, meterRef, closeMeter);
  const flow = useRef<HTMLDivElement>(null);
  // Elapsed is a clock reading and belongs on the tick. Throughput is not: it
  // follows the deltas themselves, and expires rather than being re-derived.
  const tps = useRate(s.outWindow, s.running);
  const live = useLiveWork(s.items, s.running);
  // The shape of the last minute, kept while a turn runs. A number alone says
  // how fast it is now; the line says whether it is climbing, stalling or
  // arriving in bursts, which is the question someone watching a run has.
  const trail = useTrail(tps, s.running);
  // What this turn has actually put on the wire: input counted whether or not
  // the prefix cache took it, and output as it comes back.
  const sent = s.metrics.hit + s.metrics.miss;
  const received = s.metrics.out + s.outLive;
  const speed = useMemo(() => speedOf(traj.rows), [traj.rows]);

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
    setStatus((prev) => (prev && JSON.stringify(prev) === JSON.stringify(next) ? prev : next));
  }, []);

  const refreshStatus = useCallback(() => {
    port.status().then(applyStatus).catch(() => {});
  }, [port, applyStatus]);

  // A hole in the stream is the transport's fact; which authority answers it is
  // each model's own. The transcript is rebuilt from /history, the run graph
  // from the read the kernel rebuilds — one gap, two different re-reads.
  useEffect(
    () =>
      port.subscribe(
        (ev) => {
          dispatch(ev);
          trajDispatch(ev);
          // A server finishing its handshake changes what /mcp answers, and this
          // is the only precise signal for it — the turn boundary below is the
          // fallback for changes that arrive without an event.
          if (ev.kind === "mcp_surface_ready" || ev.kind === "extension_status") reloadMcp();
          if (ev.kind === "todo_progress") void refreshTodos(port, dispatch);
          // A prompt opening or closing changes who the turn is waiting on, and
          // the kernel answers that in /status rather than in the frame. Same
          // shape the queue uses: the event says something moved, the read says
          // what is true. Without it the run keeps glowing until the next poll.
          if (ev.kind === "approval_request" || ev.kind === "ask_request") refreshStatus();
          // Settling one is the other half of the same move, and the receipt
          // rides as a field rather than a kind of its own.
          else if ("decisionReceipt" in ev && ev.decisionReceipt) refreshStatus();
        },
        () => {
          rebuild();
        },
      ),
    [port, reloadMcp, rebuild, refreshStatus],
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
    port.checkpoints().then((cps) => alive && setCheckpoints(cps)).catch(() => {});
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
      setStatus(st);
      dispatch(totalsOf(st) as never);
    });
    return () => {
      alive = false;
    };
  }, [port]);

  // The wallet only moves when a turn spends, so its clock is the turn — not
  // /status's 250ms poll, which is what it used to ride. A provider with no
  // wallet endpoint answers absent, which renders as nothing.
  const [wallet, setWallet] = useState<Wallet>(ABSENT);
  const refreshWallet = useCallback(() => {
    port
      .balance()
      .then((reading) => setWallet(reading ? { kind: "read", reading } : ABSENT))
      .catch((e) => setWallet({ kind: "unread", why: reason(e) }));
  }, [port]);

  useEffect(() => {
    if (!s.running) {
      refreshWallet();
      void refreshTodos(port, dispatch);
    }
  }, [s.running, refreshWallet, port]);

  useEffect(() => {
    if (pulse) refreshStatus();
  }, [pulse, refreshStatus]);

  const fail = useCallback((e: unknown) => {
    // A refusal carries a code; say() turns it into this window's language.
    // Anything else is an ordinary failure and prints as itself.
    dispatch({ kind: "__error", text: reason(e) } as never);
  }, []);

  // Everything on screen belongs to one session; when the kernel moves this
  // pane to another one — a switch, a new session, a rewind — all of it has to
  // be re-read rather than patched.
  const reloadSession = useCallback(() => {
    trajDispatch({ kind: "__clear" } as never);
    port.trajectory().then(replayTrajectory).catch(() => {});
    port.checkpoints().then(setCheckpoints).catch(() => setCheckpoints([]));
    // Two reads, the same way the first mount takes them: the record does not
    // wait behind the numbers over it.
    port.history().then((msgs) => {
      const r = fromHistory(msgs);
      dispatch({ kind: "__restore", ...r });
    });
    port.status().then((st) => {
      applyStatus(st);
      dispatch(totalsOf(st) as never);
    });
    refreshWallet();
    onSessionChanged();
  }, [port, applyStatus, refreshWallet, onSessionChanged, replayTrajectory]);

  const { onPrepareRewind, onCommitRewind, onUndoRewind, onPrepareFileRevert, onCommitFileRevert } = useRewindActions(port, reloadSession);

  // Both of these read only the user and tool cards, so they key off the
  // revision rather than the items array: a streamed answer leaves every card
  // they look at untouched, and recomputing them per chunk is the whole reason
  // a long session used to slow down. eslint would want `s.items` in the deps;
  // `s.revision` is the narrower truth. Same for the rail's two panels below.
  /* eslint-disable react-hooks/exhaustive-deps */
  const paired = useMemo(() => pairCheckpoints(s.items, checkpoints), [s.revision, checkpoints]);
  const rail = useMemo(() => railOf(s.items, s.executions), [s.revision, s.executions]);

  // Sub-agents and background processes: built as panels, never drawn.
  const [deck, setDeck] = useState<Deck>("");
  // The list belongs above the line that narrates the turn, because that is
  // what it is about. Plan was built and only ever mounted inside an inspector
  // nothing renders, which is why it had never been seen.
  const todoOpen = s.plan.length > 0 && tab === "flow";
  const [todoShown, setTodoShown] = useState(false);
  const jobs = status?.jobs ?? NO_JOBS;
  const counts = useMemo(() => {
    let steps = 0;
    let steer = 0;
    for (const i of s.items) {
      if (i.t === "tool") steps++;
      else if (i.t === "user" && i.pending) steer++;
    }
    return { steps, steer };
  }, [s.revision]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // An MCP server connects lazily and fails at first use, so a turn boundary is
  // also when its status can have changed — no timer of its own needed.
  useEffect(() => {
    reloadMcp();
    // A finished turn is exactly when the kernel has one more checkpoint.
    port.checkpoints().then(setCheckpoints).catch(() => {});
    port.changes().then(setTree).catch(() => setTree(null));
  }, [reloadMcp, port, status?.sessionPath, s.running]);

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
    port.context().then(setCtx).catch(() => setCtx(null));
  }, [port, roundTrips, folds, status?.sessionPath, s.running]);

  // The sidebar has to hear about this pane's session twice: when the first
  // turn mints the file (before that there is no row to show) and when the turn
  // ends (that is when it has a generated title and a turn count). Without it a
  // brand-new conversation only appeared in the tree once its pane was closed.
  useEffect(() => {
    onSessionChanged();
  }, [status?.sessionPath, s.running, onSessionChanged]);

  // /status is the only source for background jobs and for settings the run does
  // not echo, so a live turn has to re-read it rather than infer from events.
  useEffect(() => {
    if (!s.running || !visible) return;
    const tick = () => refreshStatus();
    tick();
    const t = setInterval(tick, 250);
    return () => clearInterval(t);
  }, [s.running, visible, refreshStatus]);

  useEffect(() => {
    void port.surfaceSlots().then(setSlots).catch(() => setSlots({}));
  }, [port]);

  // Where the user put each extension surface. Updated in place: a move is the
  // user's own action, so waiting for a round trip would read as a dead click.
  const moveSurface = useCallback(
    async (ext: { pluginId: string; surfaceId: string }, slot: string) => {
      const id = `${ext.pluginId}:${ext.surfaceId}`;
      setSlots((prev) => {
        const next = { ...prev };
        if (slot) next[id] = slot;
        else delete next[id];
        return next;
      });
      await port.assignSurface(id, slot).catch(fail);
    },
    [port, fail],
  );
  // Split once per change rather than per frame: a new array each render is a
  // changed prop, and that alone would keep the rail re-rendering all turn.
  const [atComposer, inRail] = useMemo(() => {
    const at: typeof s.views = [];
    const rail: typeof s.views = [];
    for (const v of s.views) (placement(v, slots) === "composer-trailing" ? at : rail).push(v);
    return [at, rail];
  }, [s.views, slots]);

  // The wire never echoes what was typed, so the row is the client's to add —
  // and its to take back when the line did not leave. Reporting the refusal is
  // not enough on its own: the transcript would keep showing a turn that never
  // happened, and the composer was emptied on the way in.
  const submit = useCallback(
    async (text: string) => {
      const steering = s.running;
      const id = localId();
      dispatch({ kind: "__user", text, pending: steering, id } as never);
      trajDispatch({ kind: "__user", text });
      try {
        if (steering) {
          // The row is already on screen; the receipt is what gives it a name
          // to be taken back by while it waits at the tool boundary.
          const queued = await port.steer(text);
          if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: "steer" } as never);
        } else {
          await submitOrQueue(text, id);
        }
        return true;
      } catch (e) {
        dispatch({ kind: "__unsent", id } as never);
        fail(e);
        return false;
      }
    },
    [port, s.running, refreshStatus, fail],
  );

  const { queue, onQueueEdit, onQueueMove, onQueueRetry, onQueueRefresh, onQueuePause, onQueueRead, onQueueSendNow, onQueueCancel } = useQueueActions({
    port,
    dispatch,
    fail,
    moved: s.queueMoved,
    sessionPath: status?.sessionPath,
  });

  // The kernel refuses a submit it cannot start with a code, not a sentence:
  // the words are fine, the timing is not. Queueing them is what that code
  // asks for, and showing anyone the refusal instead is how a race between
  // "the turn is done" on screen and the turn actually landing became an error
  // nobody could act on.
  const submitOrQueue = useCallback(
    async (text: string, id: string) => {
      try {
        await port.submit(text);
        refreshStatus();
      } catch (e) {
        if (!(e instanceof HttpError) || e.reason?.code !== "busy.session_running") throw e;
        const queued = await port.queueFollowup(text);
        if (queued?.itemId) dispatch({ kind: "__queued", id, itemId: queued.itemId, queued: "followup" } as never);
      }
    },
    [port, refreshStatus],
  );

  const { onApprove, onFullAccess, onPlan, onForget, onExtInvoke, onExtSubmit, onAnswer } = useGateActions({
    port,
    dispatch,
    refreshStatus,
    fail,
    onRevise: () => setAskFocus((n) => n + 1),
  });

  // Every route into a view goes through one place, so the menu never grows a
  // motion language of its own: it says which view, and this says how a pane
  // changes from one to another.
  // The workbench is the one view that sits beside the conversation rather than
  // over it, because it is read against what was said. Picking it from the nav
  // opens the same dock the globe does; there was no reason for one control to
  // take the conversation away and the other to keep it.
  const showView = useCallback(
    (to: PaneView) => {
      if (to === "browser") {
        onManualBrowser?.(true);
        swapping(() => setTab("flow"), "tab");
        return;
      }
      if (to === "flow") onManualBrowser?.(false);
      swapping(() => setTab(to), "tab");
    },
    [onManualBrowser],
  );
  const find = useFind(s.items, findPulse, active, useCallback(() => showView("flow"), [showView]));

  const { quote, reply, onResend } = useReplyActions({ port, items: s.items, checkpoints, running: s.running, model: status?.label, submit, onSettings, onRunDetail: () => showView("analysis"), onError: fail });

  // Where the bottom is moves as blocks mount under it, so this only asks the
  // transcript to follow again and lets it scroll itself into place.
  const toLatest = () => setJump((n) => n + 1);

  // Who is waited on is the kernel's answer, read from the decision list it
  // already publishes. This used to compare the label on screen against two
  // Chinese literals that the reducer had written — a translation key deciding
  // whether the run reads as moving.
  const blocked = hasPendingDecision(status);
  const run = runState({ blocked, running: s.running, hasItems: s.items.length > 0, terminal: s.terminal });
  const cost = money(s.metrics.cost, s.metrics.currency);
  const cacheTokens = s.metrics.hit + s.metrics.miss;
  const cacheRate = cacheTokens > 0 ? Math.round((s.metrics.hit / cacheTokens) * 100) : null;
  const contextPercent = ctx && ctx.window > 0 ? Math.min(100, Math.round((ctx.used / ctx.window) * 100)) : null;
  const walletDisplay = wallet.kind === "read" ? wallet.reading.display : "";

  // The chrome reads the focused pane. Reporting from an effect keeps it out of
  // render, where it would set state on the parent mid-paint.
  useEffect(() => {
    onReport(rt.id, {
      status,
      title,
      steer: counts.steer,
      run,
      live: s.running || blocked,
      cost,
      contextPercent,
      context: ctx,
      mcp,
      wallet: walletDisplay,
    });
  }, [rt.id, onReport, status, title, counts.steer, run, s.running, blocked, cost, contextPercent, ctx, mcp, walletDisplay]);

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
      <div className="pviews">

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
        onUndoRewind={onUndoRewind}
        onPrepareFileRevert={onPrepareFileRevert}
        onCommitFileRevert={onCommitFileRevert}
        needsProject={needsProject}
        onOpenProject={onOpenProject}
        onKeepHere={onKeepHere}
      />
      </LiveWork.Provider>

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
        <Gutter edge="r" span={DOCK} width={dockW} label={t("调整浏览器宽度")} open={docked}
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
            changes={tree?.changes ?? []}
            running={s.running}
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
        {todoOpen && (
          <div className="studio-todo" data-open={todoShown ? "" : undefined}>
            {/* One line by default. The composer already carries the run line,
                the outbox and its own toolbar; a plan opened over all of them
                pushes the box a person types in off the bottom of a laptop. */}
            <button
              type="button"
              className="studio-todo-sum"
              data-action="plan.fold"
              aria-expanded={todoShown}
              onClick={() => setTodoShown((on) => !on)}
            >
              <StudioIcon name="list" />
              <b>{t("计划")}</b>
              <span>{stepLabel(s.plan[currentStep(s.plan)] ?? s.plan[0])}</span>
              <small>{s.plan.filter(stepDone).length}/{s.plan.length}</small>
              <StudioIcon name="down" className="studio-todo-fold" />
            </button>
            {todoShown && <div className="studio-todo-body"><Plan steps={s.plan} /></div>}
          </div>
        )}
        {tab === "flow" && (
          <div
            className="studio-runstate"
            role="status"
            aria-live="polite"
            data-waiting={blocked ? "" : undefined}
            data-idle={s.running || blocked ? undefined : ""}
          >
            <RMark />
            <span>{t(s.doing || "运行中")}</span>
            <RunTokens sent={sent} received={received} estimated={s.outLive > 0} />
          </div>
        )}
        <div className="composeaux">
          <Queue
            queue={queue}
            running={s.running}
            onRead={onQueueRead}
            onSendNow={onQueueSendNow}
            onEdit={onQueueEdit}
            onMove={onQueueMove}
            onCancel={onQueueCancel}
            onRetry={onQueueRetry}
            onRefresh={onQueueRefresh}
            onPause={onQueuePause}
          />
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
        <Composer port={port} status={status} running={s.running} quote={quote} focus={askFocus} onSubmit={submit} onChanged={refreshStatus} onError={fail} onSettings={onSettings} changeCount={tree?.repo ? tree.changes.length : 0} pulse={pulse} />
        <div className="studio-meterrail" ref={meterRef} aria-label={t("运行统计")}>
          <div className="studio-speed-anchor">
            <button
              className="studio-meter-static studio-meter-speed"
              type="button"
              aria-describedby="studio-speed-detail"
              aria-label={t("查看生成速度详情")}
            >
              <StudioIcon name="gauge" />
              <Spark points={trail} w={44} h={13} />
              <b>{tps > 0 ? tps.toFixed(1) : "—"}</b><span>tok/s</span><i data-live={s.running ? "" : undefined} aria-hidden="true" />
            </button>
            <div className="studio-speed-detail" id="studio-speed-detail" role="tooltip">
              <header><b>{t("生成速度")}</b><small>{s.running ? t("实时更新") : t("最近一轮")}</small></header>
              <dl>
                <div><dt>{t("当前速度")}</dt><dd>{tps > 0 ? `${tps.toFixed(1)} tok/s` : "—"}</dd></div>
                <div><dt>{t("整轮平均")}</dt><dd>{speed.average > 0 ? `${speed.average.toFixed(1)} tok/s` : "—"}</dd></div>
                <div><dt>{t("本轮输出")}</dt><dd>{speed.output > 0 ? t("{n} tokens", { n: speed.output.toLocaleString() }) : "—"}</dd></div>
                <div><dt>{t("模型耗时")}</dt><dd>{speed.modelSeconds > 0 ? `${speed.modelSeconds.toFixed(1)}s` : "—"}</dd></div>
              </dl>
              <p>{t("当前速度按最近 4 秒流式文本估算；整轮平均使用服务商返回的输出 Token 除以模型回合耗时。")}</p>
            </div>
          </div>
          <span className="studio-meter-static studio-meter-cache" title={cacheRate === null ? t("尚无缓存数据") : t("命中 {hit} · 未命中 {miss}", { hit: s.metrics.hit.toLocaleString(), miss: s.metrics.miss.toLocaleString() })}>
            <span>{t("缓存")}</span><b>{cacheRate === null ? "—" : `${cacheRate}%`}</b>
          </span>
          {ctx && ctx.window > 0 && (
            <div className="studio-context-anchor" data-open={meterOpen ? "" : undefined}>
              <button className="studio-meter-context" data-action="metrics.details" data-value="context" aria-expanded={meterOpen} aria-haspopup="dialog" aria-label={t("查看上下文与压缩")} onClick={() => setMeterOpen((open) => !open)}>
                <span className="studio-context-ring" style={{ "--fill": `${Math.min(100, Math.round((ctx.used / ctx.window) * 100))}%` } as CSSProperties} aria-hidden="true" />
                <span>{t("上下文")}</span><b>{Math.min(100, Math.round((ctx.used / ctx.window) * 100))}%</b>
              </button>
              <ContextSummaryCard
                className="studio-context-summary"
                context={ctx}
                mcp={mcp}
                percent={Math.min(100, Math.round((ctx.used / ctx.window) * 100))}
                onManage={() => { closeMeter(); onSettings("ext"); }}
              />
            </div>
          )}
          {cost && <span className="studio-meter-cost"><span>{t("本轮")}</span><b>{cost}</b></span>}
          {wallet.kind === "read" && <button className="studio-meter-wallet" data-action="settings.section" data-value="usage" aria-label={t("查看钱包余额")} onClick={() => onSettings("usage")}><StudioIcon name="wallet" /><b>{wallet.reading.display}</b></button>}
          <DeckChips tasks={rail.tasks} jobs={jobs} open={deck} onOpen={setDeck} onCancelJob={(id) => port.cancelJob(id).then(refreshStatus, fail)} />
        </div>
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
          <RuntimeBar notices={s.runtime} onSeen={(id) => dispatch({ kind: "__runtime_seen", id } as never)} />
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
            done={!s.running}
            posture={posture(run, blocked)}
            plan={s.plan}
            wallet={wallet}
            account={accountOf(status?.modelRef)}
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

// Panes run at the same time: a frame arriving in one must not re-render the
// others, or two live conversations cost twice what one does.
export const Pane = memo(PaneView);
