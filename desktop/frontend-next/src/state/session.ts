import type { CostCoverage, Receipt, Tool, WireEvent } from "../port/wire";
import { noteTool, type Executions } from "./executions";
import { estimateTokens, sample } from "../port/tokens";
import type { HistoryMessage } from "../port/port";
import { plural, t } from "../i18n";
import { currentStep, stepDone, stepLabel } from "./session_types";
import type { Item, Metrics, PlanStep, RememberedFact, RuntimeNotice, SessionState, TodoStatus, TurnTerminal, Waiting } from "./session_types";
import { foldUsage, quoteAmount } from "./usage";
import { setShowsReceipt, showsReceipt } from "./prefs";

// The types live next door; this stays their way in, so no reader of a
// session has to know they were split off.
import { splitProviderSearch } from "./providersearch";

export type { Item, Metrics, PlanStep, RememberedFact, RuntimeNotice, SessionState, TodoStatus, TurnTerminal, Waiting };
export { currentStep, stepDone, stepLabel };
import { askFromCall, promptOpen, prompted, sealByReceipt } from "./prompts";
import { nameTurnStart, othersSteer } from "./turn_start";
import { appendText, foldMessage, sealSay } from "./say";
import { nextId } from "./ids";
import { chipLabel, IDLE, RUNNING } from "./chip";
import { foldStall } from "./stall";
import { nameQueued } from "./queued";
import { dropTool, foldLastRead, foldTool, isSubagentProgress, mergeReads, notePhase } from "./fold";
export { chipLabel };
export { quoteAmount };
export { setShowsReceipt, showsReceipt };

// doing is what the status chip prints. These two values are also read back by
// the reducer, so they get a name: a comparison against a sentence is one copy
// pass away from never matching again, and nothing fails when it stops.
const WAITING_WORKSPACE = "等待工作区";
// What the turn is doing between a tool finishing and the model's next packet.
// The chip used to keep printing the tool that had already returned, so the
// one stretch nothing is on screen for looked like the one stretch it hung.
const WAITING_MODEL = "等待模型回复";

export const initialState: SessionState = {
  error: "",
  executions: {},
  terminal: null,
  runtime: [],
  items: [],
  entranceOwed: [],
  enteredThrough: 0,
  revision: 0,
  plan: [],
  outWindow: [],
  outLive: 0,
  metrics: { hit: 0, miss: 0, out: 0, bySource: {}, cost: 0, currency: "¥",
    prefixHash: "", prefixChanged: false, prefixReasons: [], bodyChanged: false, carriedMessages: 0, toolSchema: 0,
    estimated: false, coverage: "none", incompleteReason: "", alt: null, turn: 0, rounds: [] },
  waiting: {},
  running: false,
  doing: IDLE,
  steerQueue: [], takenBack: [], subagentPhase: {},
  awaitingTurnStart: [],
  queueMoved: 0,
  browserTabsMoved: 0,
  panels: [],
  views: [],
  takeovers: {},
};

export const localId = nextId;

// tool_dispatch and tool_result are two phases of one call; the UI shows one
// row that flips from running to settled, so they fold onto the same item.
// The end of a turn is the only authority on whether its work finished. A turn
// that died put its reason in the status label, where the next turn overwrites
// it and a long one is cut off mid-sentence — and left every dispatched call
// spinning, which reads as a hang rather than a stop. The reason belongs in the
// transcript, which is the record of what happened, and a call that never
// reported back says so on its own card.
//
// A backgrounded command is not caught by this: it answers with a result the
// moment it is launched, so its card is already settled when the turn ends.
const NO_REPORT = "这一步没有回报结果就随本轮结束了";

// A call still on its streaming placeholder is the other case, and the opposite
// claim: no arguments ever reached it, so it never ran and nothing on disk
// moved. Those are folded into one line — a turn interrupted while the model
// was writing a batch of edits left one identical sentence per abandoned call,
// burying the reason the turn ended under twenty copies of it.
function sealTurn(items: Item[], err?: string): Item[] {
  let stillborn = 0;
  const sealed: Item[] = [];
  for (const i of items) {
    // The kernel clears pending decisions when the turn ends; a history
    // rebuild must not carry these expired prompts into the next turn.
    if (promptOpen(i)) continue;
    if (i.t === "tool" && i.running && i.tool.partial) {
      stillborn++;
      continue;
    }
    sealed.push(
      i.t === "tool" && i.running
        ? { ...i, running: false, tool: { ...i.tool, err: i.tool.err || t(NO_REPORT) } }
        : i,
    );
  }
  if (stillborn > 0) {
    const text = plural(
      stillborn,
      "另有 1 个调用尚未开始，未修改任何文件",
      "另有 {n} 个调用尚未开始，均未修改任何文件",
    );
    sealed.push({ t: "notice", id: nextId(), level: "info", text });
  }
  if (!err) return sealed;
  // The kernel's own words: classifying them here would be this file guessing
  // at failures it cannot see. A turn it flagged as cancelled never gets here:
  // the flag is the identity, and its err only repeats the sentinel.
  return [...sealed, { t: "notice" as const, id: nextId(), level: "error", text: err }];
}


// The card reads the call's own arguments: they carry what was saved and how it
// will be recalled, which the tool's textual receipt does not.
function parseRemembered(tool: Tool): RememberedFact | null {
  try {
    const a = JSON.parse(tool.args ?? "{}") as Record<string, string>;
    const name = (a.name || "").trim();
    const description = (a.description || "").trim();
    if (!name && !description) return null;
    return {
      name,
      title: (a.title || "").trim() || name,
      description,
      scope: (a.scope || "project").trim(),
      activation: (a.activation || "relevant").trim(),
      body: (a.body || "").trim(),
    };
  } catch {
    return null;
  }
}


// The kernel's contract for a retry: the indicator is transient, and the next
// stream event clears it. These kinds say nothing about the connection and
// leave the wait standing; everything else is the provider having answered.
const holdsWait = new Set<string>([
  "turn_started",
  "retrying",
  "stream_attempt",
  "notice",
  "phase",
  "turn_phase",
  "mcp_surface_ready",
  "workspace_changed",
  "inbox_changed",
  "browser_tabs_changed",
  "context_maintenance",
  "extension_surface",
  "extension_status",
]);

// A streamed chunk lands on the card being written and touches nothing else, so
// it leaves the revision alone; every other event that moves the transcript
// bumps it. Wrapping the switch keeps that rule in one place instead of on each
// of its two dozen returns.
export function reduce(s: SessionState, ev: SessionEvent): SessionState {
  const next = foldStall(apply(s, ev), ev);
  if (next.items === s.items) return next;
  const streamed = ev.kind === "text" || ev.kind === "reasoning";
  const bumped = streamed ? next : { ...next, revision: next.revision + 1 };
  return { ...bumped, ...entering(s, bumped, ev) };
}

// The sequence a card was minted at. One monotonic source mints every id, so
// "this projection has already let everything through n in" is a number rather
// than a set to carry and copy.
const seqOf = (id: string) => (id.startsWith("i") ? Number(id.slice(1)) || 0 : 0);
const highest = (items: Item[]) => items.reduce((n, i) => Math.max(n, seqOf(i.id)), 0);

/** Which cards owe an entrance after this event, and how far we have let in.
 *
 *  A restored transcript owes nothing: those facts are not arriving, they
 *  arrived. Everything else earns it exactly once, at the event that first
 *  materialised the card — a delta rewriting a message it already made, a
 *  dispatch settling into a result, a block scrolling back into the document
 *  are all the same card and none of them is a new fact. The debt is carried
 *  rather than cleared here, because several events can land between two
 *  renders and the one that was never drawn still owes its entrance. */
function entering(prev: SessionState, next: SessionState, ev: SessionEvent): Partial<SessionState> {
  if (ev.kind === "__restore") return { entranceOwed: [], enteredThrough: highest(next.items) };
  const fresh = next.items.filter((i) => seqOf(i.id) > prev.enteredThrough).map((i) => i.id);
  if (fresh.length === 0) return {};
  return {
    entranceOwed: [...prev.entranceOwed, ...fresh],
    enteredThrough: Math.max(prev.enteredThrough, highest(next.items)),
  };
}

// What the wire carries, plus the turns the client owns itself: your own
// message, a decision you just made, an error with no event behind it.
export type SessionEvent =
  | WireEvent
  | { kind: "__restore"; items: Item[]; plan?: PlanStep[]; executions: Executions }
  | { kind: "__todos"; plan: PlanStep[] }
  | { kind: "__totals"; hit: number; miss: number; cost?: number; currency?: string; coverage?: CostCoverage; incompleteReason?: string }
  | { kind: "__error"; text: string }
  | { kind: "__user"; text: string; pending: boolean; id?: string }
  | { kind: "__unsent"; id: string }
  | { kind: "__queued"; id: string; itemId: string; queued: "steer" | "followup" }
  | { kind: "__decided"; id: string; verdict?: string; answers?: string[][] }
  | { kind: "__forgot"; id: string }
  | { kind: "__runtime_seen"; id: string }
  // The transcript has drawn these cards, so their one entrance is spent.
  // Render says when, not the animation: a card can leave the document before
  // its animation ends, and reduced motion runs no animation at all.
  | { kind: "__entered"; ids: string[] };

function apply(s: SessionState, ev: SessionEvent): SessionState {
  if (ev.kind === "__error") return { ...s, error: ev.text };
  if (ev.kind === "__entered") {
    const spent = new Set(ev.ids);
    const left = s.entranceOwed.filter((id) => !spent.has(id));
    return left.length === s.entranceOwed.length ? s : { ...s, entranceOwed: left };
  }
  if (ev.kind === "__runtime_seen") return { ...s, runtime: s.runtime.filter((n) => n.id !== ev.id) };
  if (ev.kind === "inbox_changed") return { ...s, queueMoved: s.queueMoved + 1 };
  if (ev.kind === "browser_tabs_changed") return { ...s, browserTabsMoved: s.browserTabsMoved + 1 };
  // Both event.Message emitters carry assistant text, so nothing on the wire
  // echoes what you typed — only /history has it, and only after a reload. The
  // client owns its own turn. Mid-turn input stays pending until the steer
  // event says the run consumed it at a tool boundary.
  if (ev.kind === "__user") {
    const id = ev.id ?? nextId();
    return {
      ...s,
      steerQueue: ev.pending ? [...s.steerQueue, ev.text] : s.steerQueue,
      awaitingTurnStart: ev.pending ? s.awaitingTurnStart : [...s.awaitingTurnStart, id],
      items: [...s.items, { t: "user", id, text: ev.text, pending: ev.pending }],
    };
  }
  if (ev.kind === "__queued") return nameQueued(s, ev.id, ev.itemId, ev.queued);
  // A line the kernel never took is not part of what happened, so it leaves the
  // transcript rather than sitting there looking sent. Either name identifies
  // it: the row the composer minted, or the entry the kernel queued it as —
  // the queue panel only ever knows the second.
  if (ev.kind === "__unsent") {
    return {
      ...s,
      items: s.items.filter((i) => !(i.t === "user" && (i.id === ev.id || i.itemId === ev.id))),
      takenBack: [...s.takenBack, ev.id].slice(-32),
    };
  }
  // The card stays after the fact is dropped, marked: the transcript is the
  // record of what happened, and erasing the row would erase that too.
  if (ev.kind === "__forgot") {
    return {
      ...s,
      items: s.items.map((i) => (i.t === "remember" && i.id === ev.id ? { ...i, forgotten: true } : i)),
    };
  }
  // The card reads its sealed state off the item, so the decision has to be
  // recorded here — otherwise an answered question stays answerable forever.
  if (ev.kind === "__decided") {
    // Answering hands the turn back to the tool, which may then run for a
    // minute. Leaving the label on 等你批准 says the opposite of what is
    // happening, and it is the one line the user watches to know anything is.
    const decided = s.items.find((i) => i.id === ev.id);
    // 计划卡的三个结局里只有一个是「放行」：改计划和暂不执行都在门口否决，
    // 区别只在离不离开计划模式 —— 两个都不该把标签写成某个工具正在跑。
    const halted = ev.verdict === "deny" || ev.verdict === "revise" || ev.verdict === "exit";
    const resumed = decided?.t === "approval" && !halted ? decided.a.tool || RUNNING : s.doing;
    return {
      ...s,
      doing: decided?.t === "ask" ? "运行中" : resumed,
      items: s.items.map((i) =>
        i.id !== ev.id
          ? i
          : i.t === "approval"
            ? { ...i, verdict: ev.verdict ?? "once" }
            : i.t === "ask"
              // This window's own answer, landed. The receipt it caused rides the
              // event stream and can arrive first, sealing the card as answered
              // elsewhere; the answer this window sent is the truer account.
              ? { ...i, answered: ev.answers ?? [], answeredElsewhere: false, by: undefined, said: undefined }
              : i,
      ),
    };
  }
  // A rebuild re-reads the record, and an open prompt is not in it: it is the
  // run stopped, waiting on an answer only this window can give. Overwriting it
  // left the session reading 等你决定 with nothing on screen to decide.
  if (ev.kind === "__restore") {
    // How the restored turns ended is not in the record; a live turn that
    // vanished mid-flight leaves null, which is a different answer.
    const terminal: TurnTerminal = ev.items.length ? { kind: "unread" } : s.terminal;
    return { ...s, executions: ev.executions, terminal, items: [...ev.items, ...s.items.filter(promptOpen)], plan: ev.plan ? livePlan(ev.plan) : s.plan };
  }
  // The kernel's canonical task list, asked for rather than re-derived: the
  // advances are not todo_write calls, and the refused writes are.
  if (ev.kind === "__todos") {
    return { ...s, plan: livePlan(ev.plan) };
  }
  // The session's own running totals, read back from the kernel rather than
  // restarted here: a count that begins at zero makes the next request the whole
  // sample, and one request that misses on what it just added reads as a 75%
  // session on a session the kernel has held at 96%. It arrives on its own
  // rather than with the transcript — the record is what the reader is waiting
  // for, and it must not wait behind a status read that goes to the network.
  if (ev.kind === "__totals") {
    // The kernel is the authority on how much of what it billed carries a
    // price: it holds every round, while this side has folded none of them yet.
    // A kernel that cannot say restores nothing rather than a guess — the live
    // fold takes over from the next round, which is the only thing it saw.
    return {
      ...s,
      metrics: {
        ...s.metrics,
        hit: ev.hit,
        miss: ev.miss,
        cost: ev.cost ?? s.metrics.cost,
        currency: ev.currency || s.metrics.currency,
        coverage: ev.coverage ?? s.metrics.coverage,
        incompleteReason: ev.coverage ? ev.incompleteReason ?? "" : s.metrics.incompleteReason,
      },
    };
  }
  // A wait only text or reasoning could end outlived every turn whose first
  // packet was a tool call: the retry line and its clock stayed up for the rest
  // of the turn, over calls that were running fine.
  if ((s.waiting.ttftSince || s.waiting.retry) && !holdsWait.has(ev.kind)) {
    s = { ...s, waiting: {} };
  }
  // Rides the frame that carries it, so the notice still lands in the record.
  if ("decisionReceipt" in ev && ev.decisionReceipt) s = sealByReceipt(s, ev.decisionReceipt);
  // A decision receipt is state synchronization, not conversation content.
  // The card it names already changes to its settled state, while rendering the
  // receipt again exposes the kernel's audit wording ("Decision recorded: …")
  // as a second, unexplained chat row.
  if (ev.kind === "notice" && ev.code === "decision_receipt") return s;
  switch (ev.kind) {
    case "turn_started":
      // A new turn clears how the last one ended. Terminal is a fact about the
      // turn in front of you, not a record that one ever finished — without
      // this it would be the latter, and the tick from an hour ago would still
      // be on screen over work that is running now.
      return nameTurnStart({ ...s, running: true, doing: "运行中", terminal: null, outLive: 0, turnModel: ev.modelRef || s.turnModel, waiting: { ttftSince: Date.now() } }, ev);

    case "reasoning":
      return {
        ...s,
        doing: "思考中",
        outWindow: sample(s.outWindow, estimateTokens(ev.text ?? ""), Date.now()),
        outLive: s.outLive + estimateTokens(ev.text ?? ""),
        items: appendText(s.items, ev.text ?? "", "reasoning", ev.source, ev.modelRef || s.turnModel),
      };

    case "text":
      return {
        ...s,
        doing: "正在回答",
        outWindow: sample(s.outWindow, estimateTokens(ev.text ?? ""), Date.now()),
        outLive: s.outLive + estimateTokens(ev.text ?? ""),
        items: appendText(s.items, ev.text ?? "", "text", ev.source, ev.modelRef || s.turnModel),
      };

    case "message":
      return { ...s, items: foldMessage(s.items, ev) };

    case "tool_dispatch":
      return ev.tool
        ? { ...s, doing: ev.tool.name, executions: noteTool(s.executions, ev.tool, false), items: foldTool(s.items, ev.tool, true) }
        : s;

    case "tool_progress":
      if (ev.tool && isSubagentProgress(ev.tool.name)) return { ...s, subagentPhase: notePhase(s.subagentPhase, ev.tool) };
      return ev.tool
        ? { ...s, executions: noteTool(s.executions, ev.tool, false), items: foldTool(s.items, ev.tool, true) }
        : s;

    case "tool_result": {
      if (!ev.tool) return s;
      // A failed remember is an ordinary failed tool call; only a save that
      // actually landed is worth its own card.
      const fact = ev.tool.name === "remember" && !ev.tool.err ? parseRemembered(ev.tool) : null;
      // The execution is recorded before either arm: one of them deletes the
      // card, and a card being replaced is not the call not having run.
      const executions = noteTool(s.executions, ev.tool, true);
      if (fact) {
        return { ...s, executions, items: [...dropTool(s.items, ev.tool.id), { t: "remember", id: nextId(), m: fact }] };
      }
      // A delegate keeps its own todo list, and it is not the plan the user is
      // watching. Without the parentId guard the rail flips to the subagent's
      // steps mid-turn: the user's own completed items lose their strike and
      // line one turns into somebody else's first step.
      // A refused todo_write changed no host state, so its payload is not a plan.
      const own = ev.tool.name === "todo_write" && !ev.tool.parentId && !ev.tool.err;
      const plan = (own && parsePlan(ev.tool)) || s.plan;
      // A returned call hands the turn back to the model, so the wait starts
      // again here — a sub-agent's call does not, because the parent turn was
      // never the thing waiting on it.
      const handedBack = !ev.tool.parentId && s.running;
      return {
        ...s,
        plan,
        executions,
        doing: handedBack ? WAITING_MODEL : s.doing,
        waiting: handedBack ? { ...s.waiting, ttftSince: Date.now() } : s.waiting,
        items: mergeReads(foldTool(s.items, ev.tool, false)),
      };
    }

    case "usage": {
      const u = ev.usage;
      return u ? { ...s, outLive: 0, metrics: foldUsage(s.metrics, u) } : s;
    }

    case "guardian_assessment":
      return ev.guardian
        ? { ...s, items: [...s.items, { t: "guardian", id: nextId(), g: ev.guardian }] }
        : s;

    // A surface is addressed by id, so a re-publish is an update: a status the
    // extension refreshes while it works would otherwise pile up one card per
    // tick instead of moving in place.
    case "extension_surface":
    case "extension_status": {
      const ext = ev.extension;
      if (!ext) return s;
      if (ext.kind === "panel") {
        const at = s.panels.findIndex((p) => p.pluginId === ext.pluginId && p.surfaceId === ext.surfaceId);
        if (at < 0) return { ...s, panels: [...s.panels, ext] };
        const panels = s.panels.slice();
        panels[at] = ext;
        return { ...s, panels };
      }
      if (ext.kind === "view" && ext.view?.anchor) {
        return { ...s, takeovers: { ...s.takeovers, [ext.view.anchor]: ext } };
      }
      if (ext.kind === "view") {
        const at = s.views.findIndex((v) => v.pluginId === ext.pluginId && v.surfaceId === ext.surfaceId);
        if (at < 0) return { ...s, views: [...s.views, ext] };
        const views = s.views.slice();
        views[at] = ext;
        return { ...s, views };
      }
      const at = s.items.findIndex(
        (it) => it.t === "extension" && it.ext.pluginId === ext.pluginId && it.ext.surfaceId === ext.surfaceId,
      );
      if (at < 0) return { ...s, items: [...s.items, { t: "extension", id: nextId(), ext }] };
      const items = s.items.slice();
      items[at] = { t: "extension", id: items[at].id, ext };
      return { ...s, items };
    }

    case "approval_request":
      return ev.approval ? prompted(s, "等待批准", { t: "approval", id: nextId(), a: ev.approval }) : s;

    case "ask_request":
      return ev.ask ? prompted(s, "等待确认", { t: "ask", id: nextId(), ask: ev.ask }) : s;

    case "compaction_started":
      return { ...s, items: [...s.items, { t: "compaction", id: nextId(), c: ev.compaction ?? {}, done: false }] };

    // The digest streams in while the fold runs. It accumulates on the card's
    // own summary so the finished event simply replaces it — a fold that dies
    // mid-write leaves what it had written rather than an empty placeholder.
    case "compaction_progress": {
      if (!ev.text) return s;
      const items = s.items.slice();
      for (let i = items.length - 1; i >= 0; i--) {
        const it = items[i];
        if (it.t === "compaction" && !it.done) {
          items[i] = { ...it, c: { ...it.c, summary: (it.c.summary ?? "") + ev.text } };
          break;
        }
      }
      return { ...s, items };
    }

    case "compaction_done": {
      const items = s.items.slice();
      for (let i = items.length - 1; i >= 0; i--) {
        if (items[i].t === "compaction") {
          items[i] = { t: "compaction", id: items[i].id, c: ev.compaction ?? {}, done: true };
          break;
        }
      }
      return { ...s, items };
    }

    // The quality summary is a machine record — content-free counts for
    // strategy and audit. The trajectory keeps it; the transcript gets the
    // receipt on turn_done instead, which is the one written for a person.
    case "completion_summary":
      return s;

    // A retry between two steps is the same stall as one before the first
    // packet, so it arms the wait rather than only decorating one already up —
    // a later step losing its connection used to show nothing at all.
    case "retrying":
      return {
        ...s,
        waiting: {
          ttftSince: s.waiting.ttftSince ?? Date.now(),
          retry: {
            attempt: ev.retryAttempt ?? 0,
            max: ev.retryMax ?? 0,
            scope: ev.retryScope,
            cause: ev.retryCause,
            status: ev.retryStatus,
            delayMs: ev.retryDelayMs,
            timeoutSecs: ev.retryTimeoutSecs,
            since: Date.now(),
          },
        },
      };

    case "steer": {
      const q = s.steerQueue.filter((t) => t !== ev.text);
      // The row enters where the turn read it, not where it was typed. Its seat
      // was booked at the wrong moment: the work that ran while it waited would
      // otherwise read as work done about it.
      const at = s.items.findIndex(
        (i) => i.t === "user" && i.pending && (ev.itemId ? i.itemId === ev.itemId : i.text === ev.text),
      );
      if (at < 0) return { ...s, steerQueue: q, items: othersSteer(s.items, ev) };
      const row = { ...(s.items[at] as Extract<Item, { t: "user" }>), pending: false, steer: true, via: ev.via };
      return { ...s, steerQueue: q, items: [...s.items.slice(0, at), ...s.items.slice(at + 1), row] };
    }

    case "notice": {
      const level = ev.level ?? "info";
      // Older journals stored the capability resolver's audit line as an
      // ordinary conversation notice. The resolved tool card already carries
      // the target and capability id, so rendering both produces two cards for
      // one action. Current kernels mark this operator-only; keep the prefix
      // guard so reopening an old session gets the same quiet transcript.
      if (level === "info" && ev.text?.startsWith("capability proxy:")) return s;
      // A notice about the runtime is not a turn in the conversation, so it
      // does not join the record of one. What it says is still kept: every
      // frame reaches the trajectory either way, which is where a question
      // like "why did it pick that model" is actually answered. A warning
      // needs to be seen now, so that one takes a place of its own.
      if (ev.audience === "operator") {
        if (level === "info") return s;
        // The same standing fact does not stack. This one is about how the
        // project is configured, so it is true again every turn, and a strip
        // that grew a row each time would be the noise it replaced.
        const said = (n: { code?: string; text: string; detail?: string }) =>
          `${n.code ?? n.text}\u0000${n.detail ?? ""}`;
        const fresh = { id: nextId(), level, code: ev.code, text: ev.text ?? "", detail: ev.detail };
        if (s.runtime.some((n) => said(n) === said(fresh))) return s;
        return { ...s, runtime: [...s.runtime, fresh] };
      }
      // A wait is not a fact about the turn, it is a state the turn is in.
      // The strip carries it while it lasts, and the notice that closes it
      // rewrites the card rather than stacking a second one under it — the
      // pair used to be one line that stayed on screen saying "waiting" long
      // after the wait was over.
      if (ev.code === "workspace_lease") {
        const waiting: Item = { t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code, workspaceLease: ev.workspaceLease };
        return { ...s, doing: WAITING_WORKSPACE, items: [...s.items, waiting] };
      }
      if (ev.code === "workspace_lease_resumed" || ev.code === "workspace_lease_abandoned") {
        const items = s.items.slice();
        let closed = false;
        for (let i = items.length - 1; i >= 0; i--) {
          const it = items[i];
          if (it.t === "notice" && it.code === "workspace_lease") {
            items[i] = { t: "notice", id: it.id, level, text: ev.text ?? "", detail: ev.detail, code: ev.code, workspaceLease: ev.workspaceLease };
            closed = true;
            break;
          }
        }
        if (!closed) {
          items.push({ t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code, workspaceLease: ev.workspaceLease });
        }
        return { ...s, doing: s.doing === WAITING_WORKSPACE ? RUNNING : s.doing, items };
      }
      // A retry that repeats says the same thing each time. Three identical
      // lines push whatever came before them off the screen and read as three
      // problems, so a repeat folds into the one already there.
      const last = s.items[s.items.length - 1];
      if (last?.t === "notice" && last.level === level &&
        (last.code ? last.code === ev.code : !last.code && !ev.code && last.text === (ev.text ?? "") && last.detail === ev.detail)) {
        const folded: Item = { ...last, count: (last.count ?? 1) + 1, detail: ev.detail ?? last.detail };
        return { ...s, items: [...s.items.slice(0, -1), folded] };
      }
      return {
        ...s,
        items: [
          ...s.items,
          { t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code, workspaceLease: ev.workspaceLease },
        ],
      };
    }

    case "turn_done":
      // A readiness complaint ends the turn without the work having failed, and
      // the reason is the only part worth reading. Anything but a clean finish
      // keeps the run amber rather than claiming the tick.
      // Sealing here too: a turn that ends without a closing message otherwise
      // leaves the caret blinking on a message nothing will be appended to.
      // A plan that ran to the end is spent: it says nothing the receipt does
      // not, and leaving it struck through in the rail reads as if the next
      // turn already has a plan. One that still has open steps stays — that is
      // the half the user needs.
      return {
        ...s,
        running: false,
        terminal: turnTerminal(ev),
        doing: ev.outcome === "no_progress" ? "已暂停" : ev.cancelled ? "已取消" : ev.err ? "已中断" : "已完成",
        waiting: {},
        plan: livePlan(s.plan),
        items: withReceipt(sealTurn(sealSay(s.items, true), ev.outcome === "no_progress" || ev.cancelled ? undefined : ev.err), ev.receipt),
      };

    default:
      return s;
  }
}

// turnTerminal reads how a turn ended off the frame that ended it. Four
// judgements, not a bool: a turn that stopped because obligations are missing
// delivered nothing, and folding it in with a clean finish is what let it claim
// the tick. Cancellation carries an err of its own — the kernel says so — so
// the flag is what tells a stop apart from a dropped connection.
function turnTerminal(ev: WireEvent): TurnTerminal {
  if (ev.cancelled) return { kind: "cancelled" };
  if (ev.outcome === "no_progress") return { kind: "incomplete", outcome: ev.outcome };
  if (ev.err) return { kind: "failed", err: ev.err };
  if (ev.outcome) return { kind: "incomplete", outcome: ev.outcome };
  return { kind: "completed" };
}

// withReceipt appends the turn's completion record when the kernel says it has
// something to say — and when this reader wants to read it. Whether a receipt
// has content is the kernel's answer and is not restated here; whether it is
// wanted on this screen is the reader's, and belongs where the panel widths
// already live rather than in a config the whole install shares.
//
// The check itself is untouched either way: it still runs, still decides
// readiness, and still reaches the trajectory. Only the card is withheld.
function withReceipt(items: Item[], r?: Receipt): Item[] {
  if (!r?.saysSomething || !showsReceipt()) return items;
  return [...items, { t: "receipt", id: nextId(), r }];
}

// The kernel wraps each user turn in control-plane blocks. They are
// instructions to the model, not something the user said, so they never reach
// the transcript. This list mirrors agent.TransientUserBlockTags by hand, and
// naming a subset of it is how <background-jobs> rendered as a user message:
// the same drift internal/state/history/strip.go records having had with five of
// eleven. The server strips these before /history now — this is what covers
// sessions already on disk.
const CONTROL =
  /<(reasoning-language|response-language|execution-policy|memory-update|background-jobs|active-goal|autoresearch-runtime|hook-context|available-skills|project-instructions|capability-route|interrupted-turn-recovery|workspace|scheduled-run)[\s\S]*?<\/\1>\s*/g;
const stripControl = (s: string) => s.replace(CONTROL, "").trim();

// A plan that ran to the end is spent: struck through in the rail it reads as
// if the next turn already has one. One definition, because the kernel keeps a
// finished list and both ingest paths have to draw it the same.
export function livePlan(steps: PlanStep[]): PlanStep[] {
  return steps.length > 0 && steps.every(stepDone) ? [] : steps;
}

// todo_write carries the plan as its payload; the panel needs it as state, not
// as one more line that scrolls away.
// The todos live in args; output is only a receipt ("Todos updated: 3 total").
// Returning null keeps the existing plan instead of overwriting it with junk.
export function parsePlan(tool: Tool): PlanStep[] | null {
  try {
    const v = JSON.parse(tool.args ?? "");
    const list = Array.isArray(v?.todos) ? v.todos : Array.isArray(v) ? v : null;
    if (!list) return null;
    return list.map(todoStep);
  } catch {
    return null;
  }
}

/** One row of the kernel's list, on either path it arrives by: a todo_write's
 *  arguments, or GET /todos. Status is carried, never flattened. */
export function todoStep(x: { content?: string; status?: string; activeForm?: string; level?: number }): PlanStep {
  const status: TodoStatus = x.status === "completed" || x.status === "in_progress" ? x.status : "pending";
  return { text: String(x.content ?? ""), status, activeForm: x.activeForm, level: x.level };
}

// A reload has no event stream to replay, so the transcript is rebuilt from the
// provider conversation. Control-plane turns (system, and the language preamble
// the kernel prepends to each user message) are not part of what was said. The
// task list is not rebuilt here and is not derivable here: complete_step
// advances it without writing one, and a refused todo_write writes one the
// kernel does not hold.
export function fromHistory(msgs: HistoryMessage[]): { items: Item[]; executions: Executions } {
  const out: Item[] = [];
  const calls = new Map<string, number>();
  // What /history can say about a call: that it ran, and under what name. It
  // carries no error field, so a rebuilt execution has no outcome — which is
  // the honest answer, not the answer "succeeded".
  const executions: Executions = {};
  for (const m of msgs) {
    if (m.role === "system") continue;
    if (m.role === "user") {
      // An attachment rides in as the "@path" token it was referenced by, so a
      // turn that was nothing but a dropped file still has text here. What is
      // left with none is host chrome, and so is a line the host composed —
      // the kernel says which, and drawing it would put words in your mouth.
      // Older sessions predate hostAuthored on this repair prompt. It is kernel
      // guidance, not something the person said, so keep those transcripts clean
      // after an upgrade as well as for newly written history.
      const legacyHostRepair = m.content.startsWith("The following tools are unavailable in the current workflow phase:");
      const text = m.hostAuthored || legacyHostRepair ? "" : stripControl(m.content);
      if (text) out.push({ t: "user", id: nextId(), text, msgIndex: m.msgIndex, steer: m.steer, via: m.via });
      continue;
    }
    if (m.role === "assistant") {
      if (m.content || m.reasoning) {
        // A provider-run search leaves its listing inside the assistant text —
        // that copy is the only one the next turn has — while the live stream
        // shows it as a card. Cut it back out here, or reopening the same turn
        // replaces the card with forty lines of prose.
        let reasoning = m.reasoning;
        const parts = splitProviderSearch(m.content);
        // Thinking came before the search that follows it, so it cannot ride the
        // next text part when the turn opened with a search.
        if (reasoning && (parts.length === 0 || parts[0].search)) {
          out.push({ t: "say", id: nextId(), text: "", reasoning, done: true, model: m.modelRef, thoughtMs: m.thoughtMs });
          reasoning = undefined;
        }
        for (const part of parts) {
          if (part.search) {
            out.push({
              t: "tool",
              id: nextId(),
              tool: { id: nextId(), name: "web_search", output: part.text, readOnly: true },
              running: false,
              children: [],
            });
            continue;
          }
          if (!part.text && !reasoning) continue;
          out.push({ t: "say", id: nextId(), text: part.text, reasoning, done: true, model: m.modelRef, thoughtMs: reasoning ? m.thoughtMs : undefined });
          reasoning = undefined;
        }
      }
      for (const c of m.toolCalls ?? []) {
        if (c.id) {
          calls.set(c.id, out.length);
          executions[c.id] = { name: c.name };
        }
        out.push({
          t: "tool",
          id: nextId(),
          tool: {
            id: c.id,
            name: c.name,
            args: c.arguments,
            readOnly: true,
            resolvedName: c.resolvedName,
            capabilityId: c.capabilityId,
          },
          running: false,
          children: [],
        });
      }
      continue;
    }
    if (m.role === "tool") {
      // Where the call landed is remembered when it is pushed. Searching for it
      // instead made rebuilding cost the square of the conversation's length.
      const at = m.toolCallId === undefined ? undefined : calls.get(m.toolCallId);
      if (at !== undefined) {
        const prev = out[at] as Extract<Item, { t: "tool" }>;
        const ask = prev.tool.name === "ask" && !m.toolFailed ? askFromCall(prev.tool.id ?? "", prev.tool.args) : null;
        if (ask) {
          out[at] = { t: "ask", id: prev.id, ask, answered: [], recorded: m.content };
          continue;
        }
        out[at] = {
          ...prev,
          tool: {
            ...prev.tool,
            output: m.content,
            ...(m.outputDiff ? { outputDiff: true } : {}),
            ...(m.toolFailed ? { err: m.content, refusalCode: m.toolRefusalCode } : {}),
          },
        };
      }
    }
  }
  // A reload has to fold reads the same way the live stream does, or the same
  // conversation reads differently before and after a reopen.
  const merged: Item[] = [];
  for (const it of out) {
    merged.push(it);
    foldLastRead(merged);
  }
  return { items: merged, executions };
}
