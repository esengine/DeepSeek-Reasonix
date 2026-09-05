import type { Receipt, Tool, WireEvent } from "../port/wire";
import { estimateTokens, sample } from "../port/tokens";
import type { HistoryMessage } from "../port/port";
import { plural, t } from "../i18n";
import type { Item, Metrics, PlanStep, RememberedFact, RuntimeNotice, SessionState, TurnTerminal, Waiting } from "./session_types";
import { foldUsage, quoteAmount } from "./usage";
import { showsReceipt } from "./prefs";

// The types live next door; this stays their way in, so no reader of a
// session has to know they were split off.
export type { Item, Metrics, PlanStep, RememberedFact, RuntimeNotice, SessionState, TurnTerminal, Waiting };
import { promptOpen, prompted, sealByReceipt } from "./prompts";
export { quoteAmount };
export { showsReceipt };

export const initialState: SessionState = {
  error: "",
  terminal: null,
  runtime: [],
  items: [],
  revision: 0,
  plan: [],
  outWindow: [],
  metrics: { hit: 0, miss: 0, out: 0, bySource: {}, cost: 0, currency: "¥",
    prefixHash: "", prefixChanged: false, prefixReasons: [], bodyChanged: false, carriedMessages: 0, toolSchema: 0,
    estimated: false, alt: null, turn: 0, rounds: [] },
  waiting: {},
  running: false,
  doing: "空闲",
  steerQueue: [],
  queueMoved: 0,
  panels: [],
  views: [],
  takeovers: {},
};

let seq = 0;
const nextId = () => `i${++seq}`;

// A caller that has to name the item it just added — to take it back when the
// kernel refuses it — draws from the same sequence, so the two cannot collide.
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
      "还有 1 个调用没来得及开始，它没有改动任何文件",
      "还有 {n} 个调用没来得及开始，它们没有改动任何文件",
    );
    sealed.push({ t: "notice", id: nextId(), level: "info", text });
  }
  if (!err) return sealed;
  // The kernel's own words: classifying them here would be this file guessing
  // at failures it cannot see.
  return [...sealed, { t: "notice" as const, id: nextId(), level: "error", text: err }];
}

// The wire omits `partial: false`, so spreading a full dispatch over the
// streaming placeholder would leave its flag standing and every dispatched call
// would read as one that never started. Merging through here is what lets
// sealTurn tell the two apart.
const merge = (prev: Tool, next: Tool): Tool => ({ ...prev, ...next, partial: next.partial ?? false });

function foldTool(items: Item[], tool: Tool, running: boolean): Item[] {
  // A subagent's calls carry parentId; they belong inside the task that spawned
  // them, not as siblings in the main flow.
  if (tool.parentId) {
    const at = items.findIndex((i) => i.t === "tool" && i.tool.id === tool.parentId);
    if (at >= 0) {
      const parent = items[at] as Extract<Item, { t: "tool" }>;
      const kids = parent.children.slice();
      const k = kids.findIndex((c) => c.id === tool.id);
      if (k >= 0) kids[k] = merge(kids[k], tool);
      else kids.push({ ...tool, partial: tool.partial ?? false });
      const next = items.slice();
      next[at] = { ...parent, children: kids };
      return next;
    }
  }
  const key = tool.id;
  if (key) {
    const at = items.findIndex((i) => i.t === "tool" && i.tool.id === key);
    if (at >= 0) {
      const prev = items[at] as Extract<Item, { t: "tool" }>;
      const next = items.slice();
      next[at] = { ...prev, tool: merge(prev.tool, tool), running };
      return next;
    }
  }
  return [...items, { t: "tool", id: nextId(), tool: { ...tool, partial: tool.partial ?? false }, running, children: [] }];
}

// dropTool removes the dispatch row a specialised card replaces, so the same
// call is not shown twice once its result arrives.
function dropTool(items: Item[], id?: string): Item[] {
  if (!id) return items;
  return items.filter((i) => !(i.t === "tool" && i.tool.id === id));
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

// Settles the message still being written. turn_done settles every open one:
// a tool call between two answers leaves the earlier card unsealed forever, and
// an unsealed card keeps a caret blinking and its reveal clock running on text
// nothing will add to. Returning the same array keeps every card's memo intact.
function sealSay(items: Item[], all = false): Item[] {
  let next: Item[] | null = null;
  for (let i = items.length - 1; i >= 0; i--) {
    const it = items[i];
    if (it.t !== "say" || it.done) continue;
    next ??= items.slice();
    const ran = it.thoughtMs ?? (it.reasoning ? Date.now() - (thoughtSince.get(it.id) ?? Date.now()) : undefined);
    thoughtSince.delete(it.id);
    next[i] = { ...it, done: true, thoughtMs: ran };
    if (!all) break;
  }
  return next ?? items;
}

// read_file is the most-called tool by a wide margin — 96 calls across a sample
// of recent sessions, against 47 for bash. One card each is the noise the spec
// collapses into a single manifest. Merging happens here rather than at render
// time so each card keeps a stable identity and stays memoised.
// The spec's manifest is one step for a whole run of lookups, not for reads
// alone: its own fixture folds grep and glob rows in beside the files. A group
// still has to be anchored by a read — a lone grep is better served by its own
// excerpt list than by a row that says only how many times it matched.
const LOOKUP = new Set(["read_file", "grep", "glob", "ls"]);

const lookup = (i: Item | undefined) =>
  i?.t === "tool" && !i.running && LOOKUP.has(i.tool.name) && i.children.length === 0;

const toolOf = (i: Item) => (i as Extract<Item, { t: "tool" }>).tool;

// foldLastRead folds the item just appended into the one before it, in place,
// and says whether it did. Rebuilding a whole transcript calls it once per item,
// so it must not copy the list it is folding.
function foldLastRead(items: Item[]): boolean {
  const n = items.length;
  const last = items[n - 1];
  if (!lookup(last)) return false;
  const tool = toolOf(last);
  const prev = items[n - 2];
  if (prev?.t === "reads") {
    items[n - 2] = { ...prev, tools: [...prev.tools, tool] };
    items.length = n - 1;
    return true;
  }
  if (lookup(prev) && (tool.name === "read_file" || toolOf(prev).name === "read_file")) {
    items[n - 2] = { t: "reads", id: prev.id, tools: [toolOf(prev), tool] };
    items.length = n - 1;
    return true;
  }
  return false;
}

function mergeReads(items: Item[]): Item[] {
  const next = items.slice();
  return foldLastRead(next) ? next : items;
}

function appendText(items: Item[], text: string, field: "text" | "reasoning"): Item[] {
  const last = items[items.length - 1];
  if (last && last.t === "say" && !last.done) {
    const next = items.slice();
    // Thinking runs until the first answer token, so the clock is the gap
    // between the two streams, not the length of either.
    const stop = field === "text" && last.thoughtMs === undefined && last.reasoning;
    next[next.length - 1] = {
      ...last,
      [field]: (last[field] ?? "") + text,
      ...(stop ? { thoughtMs: Date.now() - (thoughtSince.get(last.id) ?? Date.now()) } : null),
    };
    return next;
  }
  const id = nextId();
  if (field === "reasoning") thoughtSince.set(id, Date.now());
  return [...items, { t: "say", id, text: "", done: false, [field]: text }];
}

// Keyed by item id rather than carried on the item: a start time is not part of
// what the card renders, and putting it there would make every append rewrite it.
const thoughtSince = new Map<string, number>();

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
  "context_maintenance",
  "extension_surface",
  "extension_status",
]);

// A streamed chunk lands on the card being written and touches nothing else, so
// it leaves the revision alone; every other event that moves the transcript
// bumps it. Wrapping the switch keeps that rule in one place instead of on each
// of its two dozen returns.
export function reduce(s: SessionState, ev: SessionEvent): SessionState {
  const next = apply(s, ev);
  if (next.items === s.items) return next;
  const streamed = ev.kind === "text" || ev.kind === "reasoning";
  return streamed ? next : { ...next, revision: next.revision + 1 };
}

// What the wire carries, plus the turns the client owns itself: your own
// message, a decision you just made, an error with no event behind it.
export type SessionEvent =
  | WireEvent
  | { kind: "__restore"; items: Item[]; plan: PlanStep[] }
  | { kind: "__totals"; hit: number; miss: number; cost?: number }
  | { kind: "__error"; text: string }
  | { kind: "__user"; text: string; pending: boolean; id?: string }
  | { kind: "__unsent"; id: string }
  | { kind: "__queued"; id: string; itemId: string; queued: "steer" | "followup" }
  | { kind: "__decided"; id: string; verdict?: string; answers?: string[][] }
  | { kind: "__forgot"; id: string }
  | { kind: "__runtime_seen"; id: string };

function apply(s: SessionState, ev: SessionEvent): SessionState {
  if (ev.kind === "__error") return { ...s, error: ev.text };
  if (ev.kind === "__runtime_seen") return { ...s, runtime: s.runtime.filter((n) => n.id !== ev.id) };
  if (ev.kind === "inbox_changed") return { ...s, queueMoved: s.queueMoved + 1 };
  // Both event.Message emitters carry assistant text, so nothing on the wire
  // echoes what you typed — only /history has it, and only after a reload. The
  // client owns its own turn. Mid-turn input stays pending until the steer
  // event says the run consumed it at a tool boundary.
  if (ev.kind === "__user") {
    return {
      ...s,
      steerQueue: ev.pending ? [...s.steerQueue, ev.text] : s.steerQueue,
      items: [...s.items, { t: "user", id: ev.id ?? nextId(), text: ev.text, pending: ev.pending }],
    };
  }
  // The kernel answers a queued line with the id it queued it under. The row
  // is already on screen by then — this is what gives it a name to be taken
  // back by, and losing that name is the same as losing the button.
  if (ev.kind === "__queued") {
    return {
      ...s,
      items: s.items.map((i) =>
        i.t === "user" && i.id === ev.id ? { ...i, itemId: ev.itemId, queued: ev.queued, pending: true } : i,
      ),
    };
  }
  // A line the kernel never took is not part of what happened, so it leaves the
  // transcript rather than sitting there looking sent. Either name identifies
  // it: the row the composer minted, or the entry the kernel queued it as —
  // the queue panel only ever knows the second.
  if (ev.kind === "__unsent") {
    return {
      ...s,
      items: s.items.filter((i) => !(i.t === "user" && (i.id === ev.id || i.itemId === ev.id))),
      steerQueue: s.steerQueue,
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
    const resumed = decided?.t === "approval" && !halted ? decided.a.tool || "运行中" : s.doing;
    return {
      ...s,
      doing: decided?.t === "ask" ? "运行中" : resumed,
      items: s.items.map((i) =>
        i.id !== ev.id
          ? i
          : i.t === "approval"
            ? { ...i, verdict: ev.verdict ?? "once" }
            : i.t === "ask"
              ? { ...i, answered: ev.answers ?? [] }
              : i,
      ),
    };
  }
  // A rebuild re-reads the record, and an open prompt is not in it: it is the
  // run stopped, waiting on an answer only this window can give. Overwriting it
  // left the session reading 等你决定 with nothing on screen to decide.
  if (ev.kind === "__restore") {
    return { ...s, items: [...ev.items, ...s.items.filter(promptOpen)], plan: ev.plan };
  }
  // The session's own running totals, read back from the kernel rather than
  // restarted here: a count that begins at zero makes the next request the whole
  // sample, and one request that misses on what it just added reads as a 75%
  // session on a session the kernel has held at 96%. It arrives on its own
  // rather than with the transcript — the record is what the reader is waiting
  // for, and it must not wait behind a status read that goes to the network.
  if (ev.kind === "__totals") {
    return { ...s, metrics: { ...s.metrics, hit: ev.hit, miss: ev.miss, cost: ev.cost ?? s.metrics.cost } };
  }
  // A wait only text or reasoning could end outlived every turn whose first
  // packet was a tool call: the retry line and its clock stayed up for the rest
  // of the turn, over calls that were running fine.
  if ((s.waiting.ttftSince || s.waiting.retry) && !holdsWait.has(ev.kind)) {
    s = { ...s, waiting: {} };
  }
  // Rides the frame that carries it, so the notice still lands in the record.
  if ("decisionReceipt" in ev && ev.decisionReceipt) s = sealByReceipt(s, ev.decisionReceipt);
  switch (ev.kind) {
    case "turn_started":
      // A new turn clears how the last one ended. Terminal is a fact about the
      // turn in front of you, not a record that one ever finished — without
      // this it would be the latter, and the tick from an hour ago would still
      // be on screen over work that is running now.
      return { ...s, running: true, doing: "运行中", terminal: null, waiting: { ttftSince: Date.now() } };

    case "reasoning":
      return {
        ...s,
        doing: "思考中",
        outWindow: sample(s.outWindow, estimateTokens(ev.text ?? ""), Date.now()),
        items: appendText(s.items, ev.text ?? "", "reasoning"),
      };

    case "text":
      return {
        ...s,
        doing: "正在回答",
        outWindow: sample(s.outWindow, estimateTokens(ev.text ?? ""), Date.now()),
        items: appendText(s.items, ev.text ?? "", "text"),
      };

    case "message":
      return { ...s, items: sealSay(s.items) };

    case "tool_dispatch":
      return ev.tool
        ? { ...s, doing: ev.tool.name, items: foldTool(s.items, ev.tool, true) }
        : s;

    case "tool_progress":
      return ev.tool ? { ...s, items: foldTool(s.items, ev.tool, true) } : s;

    case "tool_result": {
      if (!ev.tool) return s;
      // A failed remember is an ordinary failed tool call; only a save that
      // actually landed is worth its own card.
      const fact = ev.tool.name === "remember" && !ev.tool.err ? parseRemembered(ev.tool) : null;
      if (fact) {
        return { ...s, items: [...dropTool(s.items, ev.tool.id), { t: "remember", id: nextId(), m: fact }] };
      }
      // A delegate keeps its own todo list, and it is not the plan the user is
      // watching. Without the parentId guard the rail flips to the subagent's
      // steps mid-turn: the user's own completed items lose their strike and
      // line one turns into somebody else's first step.
      const own = ev.tool.name === "todo_write" && !ev.tool.parentId;
      const plan = (own && parsePlan(ev.tool)) || s.plan;
      return { ...s, plan, items: mergeReads(foldTool(s.items, ev.tool, false)) };
    }

    case "usage": {
      const u = ev.usage;
      return u ? { ...s, metrics: foldUsage(s.metrics, u) } : s;
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
      return ev.approval ? prompted(s, "等你批准", { t: "approval", id: nextId(), a: ev.approval }) : s;

    case "ask_request":
      return ev.ask ? prompted(s, "等你决定", { t: "ask", id: nextId(), ask: ev.ask }) : s;

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
            since: s.waiting.retry?.since ?? Date.now(),
          },
        },
      };

    case "steer": {
      const q = s.steerQueue.filter((t) => t !== ev.text);
      const items = s.items.map((i) =>
        i.t === "user" && i.pending && i.text === ev.text ? { ...i, pending: false } : i,
      );
      return { ...s, steerQueue: q, items };
    }

    case "notice": {
      const level = ev.level ?? "info";
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
        const waiting: Item = { t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code };
        return { ...s, doing: "等待工作区", items: [...s.items, waiting] };
      }
      if (ev.code === "workspace_lease_resumed" || ev.code === "workspace_lease_abandoned") {
        const items = s.items.slice();
        let closed = false;
        for (let i = items.length - 1; i >= 0; i--) {
          const it = items[i];
          if (it.t === "notice" && it.code === "workspace_lease") {
            items[i] = { t: "notice", id: it.id, level, text: ev.text ?? "", detail: ev.detail, code: ev.code };
            closed = true;
            break;
          }
        }
        if (!closed) {
          items.push({ t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code });
        }
        return { ...s, doing: s.doing === "等待工作区" ? "运行中" : s.doing, items };
      }
      // A retry that repeats says the same thing each time. Three identical
      // lines push whatever came before them off the screen and read as three
      // problems, so a repeat folds into the one already there.
      const last = s.items[s.items.length - 1];
      if (last?.t === "notice" && last.code && last.code === ev.code && last.level === level) {
        const folded: Item = { ...last, count: (last.count ?? 1) + 1, detail: ev.detail ?? last.detail };
        return { ...s, items: [...s.items.slice(0, -1), folded] };
      }
      return {
        ...s,
        items: [
          ...s.items,
          { t: "notice", id: nextId(), level, text: ev.text ?? "", detail: ev.detail, code: ev.code },
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
        doing: ev.err ? "已中断" : "已完成",
        waiting: {},
        plan: s.plan.length > 0 && s.plan.every((p) => p.done) ? [] : s.plan,
        items: withReceipt(sealTurn(sealSay(s.items, true), ev.err), ev.receipt),
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
// the same drift internal/history/strip.go records having had with five of
// eleven. The server strips these before /history now — this is what covers
// sessions already on disk.
const CONTROL =
  /<(reasoning-language|response-language|execution-policy|memory-update|background-jobs|active-goal|autoresearch-runtime|hook-context|available-skills|project-instructions|capability-route|interrupted-turn-recovery|workspace)[\s\S]*?<\/\1>\s*/g;
const stripControl = (s: string) => s.replace(CONTROL, "").trim();

// todo_write carries the plan as its payload; the panel needs it as state, not
// as one more line that scrolls away.
// The todos live in args; output is only a receipt ("Todos updated: 3 total").
// Returning null keeps the existing plan instead of overwriting it with junk.
export function parsePlan(tool: Tool): PlanStep[] | null {
  try {
    const v = JSON.parse(tool.args ?? "");
    const list = Array.isArray(v?.todos) ? v.todos : Array.isArray(v) ? v : null;
    if (!list) return null;
    return list.map((x: { content?: string; status?: string }) => ({
      text: String(x.content ?? ""),
      done: x.status === "completed",
    }));
  } catch {
    return null;
  }
}

// The listing the kernel writes for the model: "- **title**" and, indented under
// it, "<url>". Only a run of exactly that gets lifted out of the prose — reading
// a paragraph as a result list is worse than leaving the list in place.
const SEARCH_TITLE = /^-\s+\*\*.+\*\*\s*$/;
const SEARCH_URL = /^\s+<\S+>\s*$/;

function splitProviderSearch(content: string): { text: string; search?: boolean }[] {
  const lines = content.split("\n");
  const blocks: [number, number][] = [];
  for (let i = 0; i < lines.length; ) {
    if (!SEARCH_TITLE.test(lines[i])) {
      i++;
      continue;
    }
    let end = i;
    let urls = 0;
    while (end < lines.length && (SEARCH_TITLE.test(lines[end]) || SEARCH_URL.test(lines[end]))) {
      if (SEARCH_URL.test(lines[end])) urls++;
      end++;
    }
    // A run with no source is the model's own bolded list — an answer written as
    // "- **厄瓜多尔总统将访华**" reads exactly like a result title.
    if (urls > 0) blocks.push([i, end]);
    i = end > i ? end : i + 1;
  }
  const parts: { text: string; search?: boolean }[] = [];
  const push = (from: number, to: number, search: boolean) => {
    const text = lines.slice(from, to).join("\n").trim();
    if (text) parts.push(search ? { text, search: true } : { text });
  };
  let at = 0;
  for (const [from, to] of blocks) {
    push(at, from, false);
    push(from, to, true);
    at = to;
  }
  push(at, lines.length, false);
  return parts;
}

// A reload has no event stream to replay, so the transcript is rebuilt from the
// provider conversation. Control-plane turns (system, and the language preamble
// the kernel prepends to each user message) are not part of what was said.
export function fromHistory(msgs: HistoryMessage[]): { items: Item[]; plan: PlanStep[] } {
  const out: Item[] = [];
  const calls = new Map<string, number>();
  let plan: PlanStep[] = [];
  for (const m of msgs) {
    if (m.role === "system") continue;
    if (m.role === "user") {
      // An attachment rides in as the "@path" token it was referenced by, so a
      // turn that was nothing but a dropped file still has text here. What is
      // left with none is host chrome, and that is what goes.
      const text = stripControl(m.content);
      if (text) out.push({ t: "user", id: nextId(), text });
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
          out.push({ t: "say", id: nextId(), text: "", reasoning, done: true });
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
          out.push({ t: "say", id: nextId(), text: part.text, reasoning, done: true });
          reasoning = undefined;
        }
      }
      for (const c of m.toolCalls ?? []) {
        if (c.name === "todo_write") {
          const p = parsePlan({ name: c.name, args: c.arguments, readOnly: true });
          if (p) plan = p;
        }
        if (c.id) calls.set(c.id, out.length);
        out.push({
          t: "tool",
          id: nextId(),
          tool: { id: c.id, name: c.name, args: c.arguments, readOnly: true },
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
        out[at] = { ...prev, tool: { ...prev.tool, output: m.content } };
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
  return { items: merged, plan };
}
