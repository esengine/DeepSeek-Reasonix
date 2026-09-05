import { memo, useCallback, useEffect, useMemo, useReducer, useRef, useState, useSyncExternalStore } from "react";
import { money } from "../i18n/format";
import { reason } from "../i18n/kernel";
import { t } from "../i18n";
import { hasPendingDecision, runState } from "./decisions";
import { createPortal } from "react-dom";
import { HttpError } from "../port/port";
import type { AgentPort, ApprovalVerdict, Checkpoint, ContextBreakdown, JobEntry, McpEntry, Queue as QueueSnapshot, RewindScope, SessionStatus, WorkspaceChanges } from "../port/port";
import type { RuntimeView } from "../port/hub";
import { fromHistory, initialState, localId, quoteAmount, reduce } from "../state/session";
import { pairCheckpoints } from "../state/checkpoints";
import { initialTraj, reduceTraj } from "../state/trajectory";
import { ExecutionStore } from "../state/execution";
import { Transcript } from "./Transcript";
import { Trajectory } from "./Trajectory";
import { Graph } from "./Graph";
import { Timeline } from "./Timeline";
import { Composer } from "./Composer";
import { Queue } from "./Queue";
import { SlottedView } from "./SlottedView";
import { Task } from "./Task";
import type { PlanAction } from "./cards/ApprovalCard";
import { key as slotKey, placement } from "./slots";
import { Metrics } from "./Metrics";
import { railOf } from "./panels/derive";
import { ABSENT, accountOf, type Wallet } from "./wallet";
import { arrowTabs } from "./tablist";
import { swapping } from "./swap";
import { useMarker } from "./marker";
import { tokensPerSecond } from "../port/tokens";

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
}

// A shared constant, not `?? []`: a fresh empty array every render reads as a
// changed prop to the rail below it.
const NO_JOBS: JobEntry[] = [];

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
  onSettings: () => void;
  // 这个窗口还没有人选过的项目文件夹。空转录是唯一说得出这句话的地方 —— 那里
  // 本来就在替一段还没开始的对话说明它该怎么开始。
  needsProject: boolean;
  onOpenProject: () => void;
  onKeepHere: () => void;
}

function PaneView({ port, rt, title, active, visible, sideHost, side, onFocus, onReport, onSessionChanged, pulse, onSettings, needsProject, onOpenProject, onKeepHere }: Props) {
  const [s, dispatch] = useReducer(reduce, initialState);
  const [traj, trajDispatch] = useReducer(reduceTraj, initialTraj);
  // The run graph is read, never accumulated: the kernel answers with what its
  // durable facts justify, and the stream folds onto that answer. One store per
  // port, because what it describes is that pane's session.
  const graphStore = useMemo(() => new ExecutionStore(), [port]);
  const exec = useSyncExternalStore(graphStore.subscribe, graphStore.read);
  const readGraph = useCallback(() => port.executionGraph(), [port]);
  const [status, setStatus] = useState<SessionStatus | null>(null);
  const [tab, setTab] = useState<"flow" | "line" | "traj" | "graph" | "task">("flow");
  const [pinned, setPinned] = useState(true);
  const [jump, setJump] = useState(0);
  // What the run graph asked to be shown. The counter is the request, not the
  // id: clicking the same node twice has to land twice.
  const [focus, setFocus] = useState<{ call: string; n: number } | null>(null);
  const [mcp, setMcp] = useState<McpEntry[]>([]);
  const [elapsed, setElapsed] = useState(0);
  const [askFocus, setAskFocus] = useState(0);
  const [tps, setTps] = useState(0);
  const [tree, setTree] = useState<WorkspaceChanges | null>(null);
  const [ctx, setCtx] = useState<ContextBreakdown | null>(null);
  const [checkpoints, setCheckpoints] = useState<Checkpoint[]>([]);
  const [queue, setQueue] = useState<QueueSnapshot | null>(null);
  const [slots, setSlots] = useState<Record<string, string>>({});
  const flow = useRef<HTMLDivElement>(null);
  const tabs = useRef<HTMLDivElement>(null);
  const tabMark = useMarker(tabs, '.tab[aria-selected="true"]', "x", [tab]);
  const startedAt = useRef(0);
  // Read by the 250ms tick without making it a dependency, so a delta arriving
  // between ticks does not restart the interval.
  const win = useRef(s.outWindow);
  win.current = s.outWindow;

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
        dispatch({ kind: "__restore", items: restored.items, plan: restored.plan } as never);
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
          graphStore.onEvent(ev);
          // A server finishing its handshake changes what /mcp answers, and this
          // is the only precise signal for it — the turn boundary below is the
          // fallback for changes that arrive without an event.
          if (ev.kind === "mcp_surface_ready" || ev.kind === "extension_status") reloadMcp();
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
          void graphStore.recoverFromGap(readGraph);
        },
        () => graphStore.bootstrap(readGraph),
      ),
    [port, reloadMcp, rebuild, graphStore, readGraph, refreshStatus],
  );

  useEffect(() => {
    let alive = true;
    port.trajectory().then((evs) => alive && evs.forEach((e) => trajDispatch(e))).catch(() => {});
    port.checkpoints().then((cps) => alive && setCheckpoints(cps)).catch(() => {});
    // The record and the numbers over it are two reads, not one. /status can go
    // to the network — the provider's wallet endpoint rides it — and pairing the
    // two made the conversation wait on a round trip that has nothing to do with
    // it. Whichever lands first shows what it knows.
    port.history().then((msgs) => {
      if (!alive) return;
      const restored = fromHistory(msgs);
      dispatch({ kind: "__restore", items: restored.items, plan: restored.plan } as never);
    });
    port.status().then((st) => {
      if (!alive) return;
      setStatus(st);
      dispatch({ kind: "__totals", hit: st.cacheHit, miss: st.cacheMiss, cost: quoteAmount(st.sessionCostQuote) } as never);
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
    if (!s.running) refreshWallet();
  }, [s.running, refreshWallet]);

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
    // The graph is not replayed out of the trajectory: it goes back to the
    // authority for the conversation this pane now holds, and shows nothing of
    // the one it left while that read is in flight.
    graphStore.resetForSession();
    void graphStore.bootstrap(readGraph).catch(() => {});
    port.trajectory().then((evs) => evs.forEach((e) => trajDispatch(e))).catch(() => {});
    port.checkpoints().then(setCheckpoints).catch(() => setCheckpoints([]));
    // Two reads, the same way the first mount takes them: the record does not
    // wait behind the numbers over it.
    port.history().then((msgs) => {
      const r = fromHistory(msgs);
      dispatch({ kind: "__restore", items: r.items, plan: r.plan } as never);
    });
    port.status().then((st) => {
      applyStatus(st);
      dispatch({ kind: "__totals", hit: st.cacheHit, miss: st.cacheMiss, cost: quoteAmount(st.sessionCostQuote) } as never);
    });
    refreshWallet();
    onSessionChanged();
  }, [port, applyStatus, refreshWallet, onSessionChanged, graphStore, readGraph]);

  // A rewind rewrites the transcript and the files under it, so the whole
  // session is re-read rather than patched — the same treatment a session
  // switch gets, for the same reason.
  const onPrepareRewind = useCallback((turn: number, scope: RewindScope) => port.prepareRewind(turn, scope), [port]);
  const onCommitRewind = useCallback(
    async (planId: string) => {
      const result = await port.commitRewind(planId);
      reloadSession();
      return result;
    },
    [port, reloadSession],
  );
  const onUndoRewind = useCallback((transactionId: string) => port.undoRewind(transactionId).then(reloadSession), [port, reloadSession]);
  // Reverting one file touches disk but not the transcript, so unlike a rewind
  // it does not reload the session — only the file changes.
  const onPrepareFileRevert = useCallback((path: string) => port.prepareFileRevert(path), [port]);
  const onCommitFileRevert = useCallback(
    (planId: string, resolution?: string) => port.commitFileRevert(planId, resolution),
    [port],
  );

  // Both of these read only the user and tool cards, so they key off the
  // revision rather than the items array: a streamed answer leaves every card
  // they look at untouched, and recomputing them per chunk is the whole reason
  // a long session used to slow down. eslint would want `s.items` in the deps;
  // `s.revision` is the narrower truth. Same for the rail's two panels below.
  /* eslint-disable react-hooks/exhaustive-deps */
  const paired = useMemo(() => pairCheckpoints(s.items, checkpoints), [s.revision, checkpoints]);
  const rail = useMemo(() => railOf(s.items), [s.revision]);
  const counts = useMemo(() => {
    let steps = 0;
    let steer = 0;
    for (const i of s.items) {
      if (i.t === "tool") steps++;
      else if (i.t === "user" && i.pending) steer++;
    }
    return { steps, steer };
  }, [s.revision]);
  // The ask the task view heads itself with: the latest thing you actually sent,
  // not a queued line the kernel has not been given yet.
  const ask = useMemo(() => {
    for (let i = s.items.length - 1; i >= 0; i--) {
      const it = s.items[i];
      if (it.t === "user" && !it.pending) return it.text;
    }
    return "";
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
    if (!s.running) {
      startedAt.current = 0;
      setTps(0);
      return;
    }
    if (!startedAt.current) startedAt.current = Date.now();
    const t = setInterval(() => {
      setElapsed((Date.now() - startedAt.current) / 1000);
      setTps(tokensPerSecond(win.current, Date.now()));
      refreshStatus();
    }, 250);
    return () => clearInterval(t);
  }, [s.running, refreshStatus]);

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

  // The queue as the kernel holds it. The frame says only that it moved, so the
  // answer is read back whole — which is also what puts another window's lines,
  // and the CLI's, in front of this one. The optimistic rows say only what was
  // sent from here, and they do not survive a reload.
  useEffect(() => {
    port.queue().then(setQueue).catch(() => setQueue(null));
  }, [port, s.queueMoved, status?.sessionPath]);

  const onQueueEdit = useCallback((id: string, text: string) => void port.editQueued(id, text).catch(fail), [port, fail]);
  const onQueueMove = useCallback((id: string, to: number) => void port.moveQueued(id, to).catch(fail), [port, fail]);
  const onQueueRetry = useCallback((id: string) => void port.retryQueued(id).catch(fail), [port, fail]);
  const onQueueRefresh = useCallback((id: string) => void port.refreshQueued(id).catch(fail), [port, fail]);
  const onQueuePause = useCallback((on: boolean) => void port.setQueuePaused(on).catch(fail), [port, fail]);
  const onQueueRead = useCallback((id: string) => port.readQueued(id), [port]);
  // The panel knows the entry, never the row the composer minted for it, so
  // taking one back here has to name it the way the kernel does.
  const onQueueCancel = useCallback(
    (itemId: string) => {
      port
        .cancelQueued(itemId)
        .then(() => dispatch({ kind: "__unsent", id: itemId } as never))
        .catch(fail);
    },
    [port, fail],
  );

  // Cancelling is only meaningful before the turn reads the line. After that
  // the kernel refuses and says so, and the row stops calling itself queued on
  // its own — the steer event that made it too late is also what clears it.
  const onCancelQueued = useCallback(
    (rowId: string, itemId: string) => {
      port
        .cancelQueued(itemId)
        .then(() => dispatch({ kind: "__unsent", id: rowId } as never))
        .catch(fail);
    },
    [port, fail],
  );

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

  // Transcript rows are memoised on their item; a callback rebuilt every render
  // would defeat that on the two cards that take one.
  const onApprove = useCallback(
    (itemId: string, id: string, v: ApprovalVerdict) => {
      dispatch({ kind: "__decided", id: itemId, verdict: v } as never);
      port.approve(id, v).then(refreshStatus).catch(fail);
    },
    [port, refreshStatus, fail],
  );

  // The plan gate has three outcomes the kernel keeps apart, and only one of
  // them is "allow". The other two both deny at the gate and differ in where
  // they leave you: revise stays in plan mode so the next thing you type is
  // feedback on the plan, exit leaves it. Leaving plan mode is the frontend's
  // job — the kernel does not clear the flag when a plan is approved, which is
  // why the chat TUI clears it here too. Studio never did, so approving a plan
  // executed it and then planned the next turn all over again.
  const onPlan = useCallback(
    (itemId: string, id: string, action: PlanAction) => {
      dispatch({ kind: "__decided", id: itemId, verdict: action } as never);
      // The three outcomes are three kernel transitions, so they go back whole
      // rather than as an allow/deny pair. The kernel moves the lifecycle
      // itself — setting plan mode from here would race its own transition — and
      // a stale decision is ordinary concurrency: say so, then re-read the
      // projection instead of binding this answer to whatever is open now.
      port.planDecision(id, action).catch(fail).then(refreshStatus);
      // Revising is done by talking, so put the cursor where the talking happens.
      if (action === "revise") setAskFocus((n) => n + 1);
    },
    [port, refreshStatus, fail],
  );

  // Taking it back is only cheap while the conversation that caused it is still
  // on screen; the card stays, marked, so the record of what happened survives.
  const onForget = useCallback(
    (itemId: string, name: string) => {
      port.forgetMemory(name).then(() => dispatch({ kind: "__forgot", id: itemId } as never)).catch(fail);
    },
    [port, fail],
  );

  // An extension action reports back in its own words. Surfacing the result as
  // a notice keeps it in the transcript where the card that offered it sits.
  const onExtInvoke = useCallback(
    (name: string) => {
      port
        .invokeExtensionAction(name)
        .then((message) => {
          if (message.trim()) dispatch({ kind: "notice", level: "info", text: message });
        })
        .catch(fail);
    },
    [port, fail],
  );

  const onExtSubmit = useCallback(
    (pluginId: string, surfaceId: string, values: Record<string, unknown>) => {
      port.submitExtensionForm(pluginId, surfaceId, values).catch(fail);
    },
    [port, fail],
  );

  const onAnswer = useCallback(
    (itemId: string, id: string, answers: { questionId: string; selected: string[] }[]) => {
      dispatch({ kind: "__decided", id: itemId, answers: answers.map((a) => a.selected) } as never);
      void port.answer(id, answers).catch(fail);
    },
    [port, fail],
  );

  // Both graph views hand back the same gesture — show me the call behind this
  // node — and it is two commits' worth of work in one: the view transition
  // defers what it is given, so asking for the call before the tab switch would
  // spend the request on a pane that is still off screen.
  const toCall = useCallback(
    (call: string) =>
      swapping(() => {
        setTab("flow");
        setFocus((was) => ({ call, n: (was?.n ?? 0) + 1 }));
      }, "tab"),
    [],
  );

  // The timeline's tab is drawn only while the run has a graph, so a session
  // that delegated nothing is not offered an empty page. Leaving someone parked
  // on a tab that just lost its button is the other half of that.
  useEffect(() => {
    if (tab === "line" && exec.graph.nodes.length === 0) setTab("flow");
  }, [tab, exec.graph.nodes.length]);

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

  // The chrome reads the focused pane. Reporting from an effect keeps it out of
  // render, where it would set state on the parent mid-paint.
  useEffect(() => {
    onReport(rt.id, { status, title, steer: counts.steer, run, live: s.running || blocked, cost });
  }, [rt.id, onReport, status, title, counts.steer, run, s.running, blocked, cost]);

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
      <div className="tabs" role="tablist" ref={tabs} onKeyDown={arrowTabs}>
        <button className="tab" role="tab" aria-selected={tab === "flow"} onClick={() => swapping(() => setTab("flow"), "tab")}>
          {t("活动")}<span className="n">{s.items.length}</span>
        </button>
        {exec.graph.nodes.length > 0 && (
          <button className="tab" role="tab" aria-selected={tab === "line"} onClick={() => swapping(() => setTab("line"), "tab")}>
            {t("时间线")}<span className="n">{exec.graph.nodes.length}</span>
          </button>
        )}
        <button className="tab" role="tab" aria-selected={tab === "traj"} onClick={() => swapping(() => setTab("traj"), "tab")}>
          {t("轨迹")}<span className="n">{traj.rows.length}</span>
        </button>
        <button className="tab" role="tab" aria-selected={tab === "graph"} onClick={() => swapping(() => setTab("graph"), "tab")}>
          {t("图")}{exec.graph.nodes.length > 0 && <span className="n">{exec.graph.nodes.length}</span>}
        </button>
                <button className="tab" role="tab" aria-selected={tab === "task"} onClick={() => swapping(() => setTab("task"), "tab")}>
          {t("任务")}{s.plan.length > 0 && <span className="n">{s.plan.filter((x) => x.done).length}/{s.plan.length}</span>}
        </button>
        {tabMark && <i className="tabmark" style={{ width: tabMark.len, transform: `translateX(${tabMark.at}px)` }} />}
      </div>

      <Transcript
        items={s.items}
        revision={s.revision}
        takeovers={s.takeovers}
        waiting={s.waiting}
        scroll={flow}
        hidden={tab !== "flow"}
        onPinned={setPinned}
        jump={jump}
        focus={focus}
        onSuggest={submit}
        onApprove={onApprove}
        onPlan={onPlan}
        onAnswer={onAnswer}
        onForget={onForget}
        onCancelQueued={onCancelQueued}
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

      {/* Mounted only while it is the tab on screen. Hiding it with an
          attribute left every row of it being rebuilt on each streamed
          delta — a second transcript's worth of work, drawn for nobody. */}
      <div className="scroll" data-pane="line" hidden={tab !== "line"}>
        {tab === "line" && <Timeline graph={exec.graph} items={s.items} onOpen={toCall} />}
      </div>

      <div className="scroll" data-pane="traj" hidden={tab !== "traj"}>
        {tab === "traj" && <Trajectory rows={traj.rows} onSave={(n, c) => port.saveText(n, c)} />}
      </div>

      <div className="scroll" data-pane="graph" hidden={tab !== "graph"}>
        {tab === "graph" && (
          <Graph
            run={exec}
            items={s.items}
            onOpen={toCall}
          />
        )}
      </div>

      <div className="scroll" data-pane="task" hidden={tab !== "task"}>
        {tab === "task" && (
          <Task
            goal={status?.goal ?? ""}
            ask={ask}
            plan={s.plan}
            rows={traj.rows}
            t0={traj.t0}
            running={s.running}
            blocked={blocked}
            elapsed={elapsed}
            onTrajectory={() => swapping(() => setTab("traj"), "tab")}
            onLatest={() => {
              swapping(() => setTab("flow"), "tab");
              toLatest();
            }}
            onSummary={() => submit(t("汇总当前进度与潜在风险，并列出接下来三步"))}
          />
        )}
      </div>

      <div className="compose">
        <button className="jump" hidden={pinned || tab !== "flow"} onClick={toLatest}>
          {t("↓ 回到最新")}
        </button>
        <span className="glowring" aria-hidden="true">
          <i />
        </span>
        {/* Everything that stacks above the input box shares one ceiling. Each
            of these was bounded on its own or not at all, and "not at all" is
            what let the queue grow until the transcript had no height left —
            the next child to do it would have been a different one. */}
        <div className="composeaux">
          <Queue
            queue={queue}
            onRead={onQueueRead}
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
        <Composer port={port} status={status} running={s.running && !blocked} focus={askFocus} onSubmit={submit} onChanged={refreshStatus} onError={fail} />
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
          {/* Something the runtime has to report about itself. It sits with the
              composer rather than in the transcript because it was not said by
              anyone in the conversation — and it is dismissible, because reading
              it is the whole of the response it needs. */}
          {s.runtime.map((n) => (
            <div key={n.id} className="rtbar" data-lvl={n.level} role="status">
              <span className="t">{[n.text, n.detail].filter(Boolean).join(" — ")}</span>
              <button onClick={() => dispatch({ kind: "__runtime_seen", id: n.id } as never)}>{t("知道了")}</button>
            </div>
          ))}
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
