import { describe, expect, it } from "vitest";
import { fromHistory, initialState, reduce, type Item, type SessionEvent, type SessionState } from "./session";
import type { HistoryMessage } from "../port/port";

// vitest runs these in Node, where there is no localStorage. The preference
// reader tolerates its absence by staying off, so a test that wants a receipt
// has to supply one.
const store = new Map<string, string>();
(globalThis as { localStorage?: unknown }).localStorage = {
  getItem: (k: string) => store.get(k) ?? null,
  setItem: (k: string, v: string) => void store.set(k, v),
  removeItem: (k: string) => void store.delete(k),
};
const showingReceipts = <T,>(body: () => T): T => body();
const hidingReceipts = <T,>(body: () => T): T => {
  store.set("rx-turn-receipt", "off");
  try {
    return body();
  } finally {
    store.delete("rx-turn-receipt");
  }
};

const run = (evs: SessionEvent[]): SessionState => evs.reduce(reduce, initialState);
const tools = (s: SessionState) => s.items.filter((i): i is Extract<Item, { t: "tool" }> => i.t === "tool");
const notices = (s: SessionState) =>
  s.items.filter((i) => i.t === "notice").map((i) => (i as Extract<Item, { t: "notice" }>).text);

// The kernel shows a card the moment a call begins, before its arguments have
// finished streaming. Only the full dispatch that follows carries args.
const partial = (id: string): SessionEvent =>
  ({ kind: "tool_dispatch", tool: { id, name: "write_file", partial: true, readOnly: false } }) as SessionEvent;

const full = (id: string): SessionEvent =>
  ({
    kind: "tool_dispatch",
    tool: { id, name: "write_file", args: '{"path":"a.js"}', readOnly: false, added: 3, removed: 0 },
  }) as SessionEvent;

const result = (id: string): SessionEvent =>
  ({
    kind: "tool_result",
    tool: { id, name: "write_file", args: '{"path":"a.js"}', output: "wrote a.js", readOnly: false },
  }) as SessionEvent;

const done = (err?: string): SessionEvent => ({ kind: "turn_done", err }) as SessionEvent;

describe("sealing a turn", () => {
  it("leaves a call that ran and reported alone", () => {
    const s = run([partial("c1"), full("c1"), result("c1"), done()]);
    expect(tools(s)).toHaveLength(1);
    expect(tools(s)[0].tool.err).toBeFalsy();
    expect(notices(s)).toHaveLength(0);
  });

  // A turn whose end never arrived — a kernel lost mid-answer — is closed when
  // the next one starts, the way its own end would have closed it: the
  // dispatched call reports, the never-sent batch is counted, nothing keeps
  // spinning through a turn that has nothing to do with it.
  it("seals a lost turn's open calls when the next turn starts", () => {
    const start = (n: number) => ({ kind: "turn_started", authoredTurn: n, msgIndex: n }) as SessionEvent;
    const s = run([start(1), partial("c1"), full("c2"), start(2)]);
    expect(s.running).toBe(true);
    expect(tools(s).filter((i) => i.running)).toHaveLength(0);
    expect(tools(s).find((i) => i.tool.id === "c2")?.tool.err).toContain("没有回报结果");
    expect(notices(s)[0]).toContain("1 个调用");
  });

  // The turn was interrupted while the model was still writing the arguments:
  // nothing was dispatched, nothing ran, and no file moved. Saying each of them
  // "reported no result" claims the opposite, and one line per abandoned call
  // buries the reason the turn ended.
  it("folds calls that never started into one line", () => {
    const s = run([partial("c1"), partial("c2"), partial("c3"), done("context canceled")]);
    expect(tools(s)).toHaveLength(0);
    expect(notices(s)).toHaveLength(2);
    expect(notices(s)[0]).toContain("3 个调用");
    expect(notices(s)[1]).toBe("context canceled"); // the kernel's own words, untouched
  });

  it("keeps the old wording for a call that was dispatched and died", () => {
    const s = run([partial("c1"), full("c1"), done("context canceled")]);
    expect(tools(s)).toHaveLength(1);
    expect(tools(s)[0].tool.err).toContain("没有回报结果");
    expect(notices(s)).toHaveLength(1); // only the kernel's error
  });

  it("tells the two apart in the same batch", () => {
    const s = run([partial("c1"), full("c1"), partial("c2"), partial("c3"), done("context canceled")]);
    expect(tools(s)).toHaveLength(1);
    expect(notices(s)[0]).toContain("2 个调用");
  });

  it("reads as one call, not as a count of one", () => {
    const s = run([partial("c1"), done("context canceled")]);
    expect(notices(s)[0]).toBe("另有 1 个调用尚未开始，未修改任何文件");
  });

  // The wire omits `partial: false`, so a fold that merely spread the full
  // dispatch would leave the placeholder's flag standing and every executed
  // call would read as one that never started.
  it("clears the streaming flag once real arguments arrive", () => {
    const progress = { kind: "tool_progress", tool: { id: "c1", name: "bash", output: "…" } } as SessionEvent;
    const s = run([partial("c1"), full("c1"), progress, done("context canceled")]);
    expect(tools(s)).toHaveLength(1);
  });
});

// The kernel flags a turn the user's own action ended. Its err is the sentinel's
// wording, which says nothing the flag does not, so it is not a failure card.
describe("a turn the user cancelled", () => {
  it("does not draw the cancellation as an error card", () => {
    const s = run([partial("c1"), { kind: "turn_done", cancelled: true, err: "context canceled" } as SessionEvent]);
    expect(s.items.filter((i) => i.t === "notice" && i.level === "error")).toHaveLength(0);
    expect(s.terminal).toEqual({ kind: "cancelled" });
    expect(s.doing).toBe("已取消");
  });

  it("still draws a genuine failure", () => {
    const s = run([{ kind: "turn_done", err: "provider down" } as SessionEvent]);
    expect(notices(s)).toEqual(["provider down"]);
  });
});

describe("rebuilding a reopened transcript", () => {
  const users = (msgs: HistoryMessage[]) =>
    fromHistory(msgs).items.filter((i): i is Extract<Item, { t: "user" }> => i.t === "user");

  // The control blocks come off before the transcript sees a turn. A turn sent
  // as a dropped file and nothing else still has the "@path" it was referenced
  // by, and keeping that row is how a message that was on screen while you sent
  // it is still there the next time the window opens.
  it("keeps a turn that was an attachment reference and no words", () => {
    const got = users([
      { role: "user", content: "@.reasonix/attachments/clipboard-1.png" },
      { role: "assistant", content: "看到了" },
    ]);
    expect(got).toHaveLength(1);
    expect(got[0].text).toBe("@.reasonix/attachments/clipboard-1.png");
  });

  it("still drops a row that is neither text nor attachment", () => {
    expect(users([{ role: "user", content: "<reasoning-language>zh</reasoning-language>" }])).toHaveLength(0);
  });

  it("leaves an ordinary turn as its text", () => {
    const got = users([{ role: "user", content: "第一句话" }]);
    expect(got).toHaveLength(1);
    expect(got[0].text).toBe("第一句话");
  });
});

describe("the turn's completion receipt", () => {
  const receipts = (s: SessionState) => s.items.filter((i) => i.t === "receipt");
  const finish = (r: unknown): SessionEvent => ({ kind: "turn_done", receipt: r }) as SessionEvent;

  it("keeps the quality summary out of the transcript", () => {
    const s = run([{ kind: "completion_summary", completion: { verdict: "partial", mutations: 2 } } as SessionEvent]);
    expect(s.items).toHaveLength(0);
  });

  it("sources a clean verdict instead of asserting it", () => {
    const s = showingReceipts(() => run([finish({
      verdict: "done", saysSomething: true,
      changes: [{ path: "parser.go", reviewed: false }],
      verifications: [{ command: "go test ./...", passed: true }],
    })]));
    expect(receipts(s)).toHaveLength(1);
  });

  it("shows the turn what it could not verify", () => {
    const s = showingReceipts(() => run([finish({
      verdict: "partial", saysSomething: true,
      gaps: [{ kind: "unreviewed_change", detail: "lexer.go" }],
    })]));
    expect(receipts(s)).toHaveLength(1);
  });

  // The kernel decides; the window does not second-guess it. A receipt with no
  // gaps and no clean verdict says only that the host could not judge, which a
  // transcript that already showed every step does not need repeated.
  it("follows the kernel on what is worth a line", () => {
    expect(receipts(run([finish({ verdict: "incomplete", saysSomething: false })]))).toHaveLength(0);
    expect(receipts(run([done()]))).toHaveLength(0);
  });
});

describe("a notice about the runtime rather than the conversation", () => {
  const notice = (audience: string | undefined, level: string, text: string): SessionEvent =>
    ({ kind: "notice", audience, level, text }) as SessionEvent;

  // The transcript is the record of a conversation. "guardian enabled ·
  // model=deepseek-v4-pro" was the first row of a session nobody had spoken in
  // yet — a fact about the assembly, rendered with the weight of a turn.
  it("keeps an assembly fact out of the transcript", () => {
    const s = run([notice("operator", "info", "guardian enabled · model=x")]);
    expect(notices(s)).toEqual([]);
    expect(s.runtime).toEqual([]);
  });

  // Dropping it entirely would trade one bug for another. A warning is the
  // runtime saying something is wrong with itself, and that has to be seen —
  // just not as something someone said.
  it("gives a runtime warning its own place, not a transcript card", () => {
    const s = run([notice("operator", "warn", "An MCP server failed to start.")]);
    expect(notices(s)).toEqual([]);
    expect(s.runtime.map((n) => n.text)).toEqual(["An MCP server failed to start."]);
  });

  it("lets a runtime warning be dismissed", () => {
    const s = run([notice("operator", "warn", "An MCP server failed to start.")]);
    const gone = reduce(s, { kind: "__runtime_seen", id: s.runtime[0].id } as SessionEvent);
    expect(gone.runtime).toEqual([]);
  });

  // Everything else is still about the conversation and still belongs in it:
  // a permission that was saved, a rewind that was undone, a plan awaiting
  // approval all answer something the user just did.
  it("leaves a conversation notice where it was", () => {
    const s = run([notice(undefined, "info", "undid last rewind")]);
    expect(notices(s)).toEqual(["undid last rewind"]);
    expect(s.runtime).toEqual([]);
  });

  it("keeps legacy capability proxy audits out of the transcript", () => {
    const s = run([notice(undefined, "info", "capability proxy: use_capability → web_fetch")]);
    expect(notices(s)).toEqual([]);
  });
});

describe("a line that is still queued", () => {
  const typed = (id: string, text: string): SessionEvent =>
    ({ kind: "__user", text, pending: true, id }) as SessionEvent;
  const rows = (st: SessionState) => st.items.filter((i): i is Extract<Item, { t: "user" }> => i.t === "user");
  const queued = (id: string, itemId: string, kind: "steer" | "followup"): SessionEvent =>
    ({ kind: "__queued", id, itemId, queued: kind }) as SessionEvent;

  // The row is on screen before the kernel has answered. The receipt is what
  // gives it a name to be taken back by, and without it the card has a button
  // it cannot press.
  it("remembers the queue id the kernel answered with", () => {
    const st = run([typed("row-1", "密码我告诉你"), queued("row-1", "inbox-9", "steer")]);
    expect(rows(st)[0].itemId).toBe("inbox-9");
    expect(rows(st)[0].pending).toBe(true);
  });

  // A whole turn refused because one is already running is queued too, and the
  // row has to say which wait it is in: the two land at different moments.
  it("marks a follow-up queued even though it was sent as a turn", () => {
    const sent = ({ kind: "__user", text: "x", pending: false, id: "row-2" }) as SessionEvent;
    const st = run([sent, queued("row-2", "inbox-10", "followup")]);
    expect(rows(st)[0].pending).toBe(true);
    expect(rows(st)[0].queued).toBe("followup");
  });

  it("takes the row away once the kernel drops it", () => {
    const waiting = run([typed("row-1", "x"), queued("row-1", "inbox-9", "steer")]);
    expect(rows(reduce(waiting, { kind: "__unsent", id: "row-1" } as SessionEvent))).toEqual([]);
  });

  // Too late is not an error state on the row: the steer event that made it too
  // late is the same one that stops it calling itself queued.
  it("stops calling itself queued when the turn reads it", () => {
    const waiting = run([typed("row-1", "x"), queued("row-1", "inbox-9", "steer")]);
    const read = reduce(waiting, { kind: "steer", text: "x" } as SessionEvent);
    expect(rows(read)[0].pending).toBe(false);
  });

  // Its seat was booked when it was typed, which is not when it happened. Every
  // sentence and tool call that ran during the wait would otherwise sort after
  // a line the model had not yet read.
  it("seats the row where the turn read it, not where it was typed", () => {
    const waiting = run([typed("row-1", "later"), queued("row-1", "inbox-9", "steer")]);
    const worked = reduce(waiting, { kind: "text", text: "meanwhile" } as SessionEvent);
    const read = reduce(worked, { kind: "steer", text: "later", itemId: "inbox-9" } as SessionEvent);
    expect(read.items[read.items.length - 1]).toMatchObject({ t: "user", text: "later", pending: false });
  });

  // The same rule for the other wait: a follow-up is a turn of its own, so its
  // row belongs at the turn the kernel started for it.
  it("seats a follow-up where its turn began", () => {
    const sent = ({ kind: "__user", text: "next", pending: false, id: "row-2" }) as SessionEvent;
    const waiting = run([sent, queued("row-2", "inbox-10", "followup")]);
    const worked = reduce(waiting, { kind: "text", text: "meanwhile" } as SessionEvent);
    const begun = reduce(worked, { kind: "turn_started", authoredTurn: 4, msgIndex: 9 } as SessionEvent);
    expect(begun.items[begun.items.length - 1]).toMatchObject({ t: "user", text: "next", pending: false, authoredTurn: 4 });
  });
});

describe("waiting for another session to finish writing", () => {
  const lease = (code: string, level: string, text: string): SessionEvent =>
    ({ kind: "notice", code, level, text }) as SessionEvent;
  const cards = (st: SessionState) => st.items.filter((i): i is Extract<Item, { t: "notice" }> => i.t === "notice");

  // The open used to be the whole surface: one card saying the session would
  // continue "when it is safe", still saying it long after it had.
  it("rewrites the waiting card in place when the wait ends", () => {
    const st = run([
      lease("workspace_lease", "warn", "Another session is writing to this workspace."),
      lease("workspace_lease_resumed", "info", "The workspace is free again; this session has continued."),
    ]);
    expect(cards(st)).toHaveLength(1);
    expect(cards(st)[0].code).toBe("workspace_lease_resumed");
    expect(cards(st)[0].level).toBe("info");
  });

  // A wait is a state, and the strip is where this interface says what a turn
  // is stopped on — the same place 等你批准 goes.
  it("says what the turn is waiting on, and stops saying it when it is not", () => {
    const waiting = run([lease("workspace_lease", "warn", "Another session is writing to this workspace.")]);
    expect(waiting.doing).toBe("等待工作区");
    const gaveUp = reduce(waiting, lease("workspace_lease_abandoned", "info", "The wait ended."));
    expect(gaveUp.doing).toBe("运行中");
    expect(cards(gaveUp)).toHaveLength(1);
  });
});

describe("repeated unclassified notices", () => {
  it("folds consecutive identical text from an older journal", () => {
    const event = { kind: "notice", level: "warn", text: "input was not accepted: this session is no longer writable — reopen it and try again" } as SessionEvent;
    const st = run([event, event, event]);
    const cards = st.items.filter((i): i is Extract<Item, { t: "notice" }> => i.t === "notice");
    expect(cards).toHaveLength(1);
    expect(cards[0].count).toBe(3);
  });
});

describe("the turn's verification receipt", () => {
  const done = (receipt: unknown): SessionEvent => ({ kind: "turn_done", receipt }) as SessionEvent;
  const card = { saysSomething: true, verdict: "unproven", gaps: [{ kind: "unverified_change" }] };
  const receipts = (s: SessionState) => s.items.filter((i) => i.t === "receipt");

  // A turn that changed files and verified none of them ends on the one card
  // that says so, so it is there unless this machine turned it off.
  it("closes the turn by default", () => {
    expect(receipts(run([done(card)]))).toHaveLength(1);
  });

  it("stays out when this machine turned it off", () => {
    expect(receipts(hidingReceipts(() => run([done(card)])))).toEqual([]);
  });

  // The kernel still decides whether a receipt has content at all; the
  // preference only decides whether a reader sees one that does.
  it("still respects the kernel's own answer", () => {
    expect(receipts(showingReceipts(() => run([done({ ...card, saysSomething: false })])))).toEqual([]);
  });
});

// Nothing on the wire echoes what the user typed, so the row is the client's
// to add — and its to take back when the line never left. Reporting the
// refusal alone would leave a turn on screen that never happened.
describe("a line the kernel refused", () => {
  const typed = (id: string, text: string): SessionEvent =>
    ({ kind: "__user", text, pending: false, id }) as SessionEvent;
  const users = (s: SessionState) =>
    s.items.filter((i) => i.t === "user").map((i) => (i as Extract<Item, { t: "user" }>).text);

  it("leaves the transcript", () => {
    const s = run([typed("u1", "first"), typed("u2", "second"), { kind: "__unsent", id: "u2" } as SessionEvent]);
    expect(users(s)).toEqual(["first"]);
  });

  it("takes back only its own row", () => {
    const s = run([typed("u1", "kept"), { kind: "__unsent", id: "nothing-by-that-name" } as SessionEvent]);
    expect(users(s)).toEqual(["kept"]);
  });
});

// The kernel emits Retrying before each backoff sleep and leaves it to the
// window to take the line down again: whatever comes back next is the proof the
// connection recovered, and it is far more often a tool call than a sentence.
describe("the retry line", () => {
  const started = (): SessionEvent => ({ kind: "turn_started" }) as SessionEvent;
  const retrying = (attempt: number, scope?: string): SessionEvent =>
    ({ kind: "retrying", retryAttempt: attempt, retryMax: 5, retryScope: scope }) as SessionEvent;

  it("comes down on the first packet of any kind, not only on text", () => {
    const s = run([started(), retrying(1, "stream"), partial("c1")]);
    expect(s.waiting).toEqual({});
  });

  it("stays up while nothing has come back", () => {
    const s = run([started(), retrying(1, "stream"), { kind: "notice", text: "x" } as SessionEvent]);
    expect(s.waiting.retry).toMatchObject({ attempt: 1, max: 5, scope: "stream" });
  });

  // Only the kernel knows whether it never got a header or lost a body it was
  // already writing out, and the two say different things to the reader.
  it("carries the kernel's own answer for which half broke", () => {
    const s = run([started(), retrying(2, "headers")]);
    expect(s.waiting.retry?.scope).toBe("headers");
  });

  it("shows a stall that starts between two steps", () => {
    const s = run([started(), { kind: "text", text: "hi" } as SessionEvent, retrying(1, "headers")]);
    expect(s.waiting.ttftSince).toBeTruthy();
    expect(s.waiting.retry?.attempt).toBe(1);
  });

  it("times the stall from its first attempt, not its latest", () => {
    const first = run([started(), retrying(1, "stream")]);
    const second = reduce(first, retrying(2, "stream"));
    expect(second.waiting.retry?.since).toBe(first.waiting.retry?.since);
  });

  it("comes down when the turn ends", () => {
    const s = run([started(), retrying(1, "headers"), done()]);
    expect(s.waiting).toEqual({});
  });
});

// The kernel holds the run open on an unanswered question and re-emits it to
// every client that attaches, precisely so a reconnected window can answer it.
// The transcript rebuild then overwrote the items wholesale — and a question is
// not in the transcript, it is the run stopped waiting on one. The session sat
// at 等你决定 with nothing on screen to decide, and the queued turn behind it
// never dispatched.
describe("a question the run is still blocked on", () => {
  const asking = (id: string): SessionEvent =>
    ({
      kind: "ask_request",
      ask: { id, questions: [{ id: "q1", prompt: "GUI 框架？", options: [{ label: "egui 0.27" }] }] },
    }) as SessionEvent;
  const approving = (id: string): SessionEvent =>
    ({ kind: "approval_request", approval: { id, tool: "write_file", subject: "src/main.rs" } }) as SessionEvent;
  const rebuild = (items: Item[] = []): SessionEvent => ({ kind: "__restore", items, plan: [], executions: {} }) as SessionEvent;
  const asks = (s: SessionState) => s.items.filter((i): i is Extract<Item, { t: "ask" }> => i.t === "ask");
  const said = (text: string): Item => ({ t: "say", id: "h1", text, reasoning: undefined, done: true }) as Item;

  it("survives the rebuild that follows a reconnect", () => {
    const s = run([asking("ask-1"), rebuild([said("有一个技术决策需要确认：")])]);
    expect(s.doing, "the header says the run is waiting on you").toBe("等待确认");
    expect(asks(s), "so the question has to still be answerable").toHaveLength(1);
    expect(asks(s)[0].ask.id).toBe("ask-1");
  });

  it("drops a cancelled ask before a rewritten user turn is restored and sent", () => {
    const cancelled = reduce(run([asking("ask-1")]), {
      kind: "turn_done", cancelled: true, err: "context canceled",
    } as SessionEvent);
    const rewritten = reduce(reduce(cancelled, rebuild([said("Earlier answer")])),
      { kind: "__user", id: "edited", text: "Rewritten question", pending: false } as SessionEvent);
    expect(rewritten.items.map((i) => i.t)).toEqual(["say", "user"]);
    expect(asks(rewritten)).toHaveLength(0);
  });

  it("removes pending prompts at turn end but keeps answered questions", () => {
    const first = run([asking("answered"), asking("pending"), approving("approval")]);
    const answered = reduce(first, {
      kind: "__decided", id: asks(first)[0].id, answers: [["egui 0.27"]],
    } as SessionEvent);
    const ended = reduce(answered, { kind: "turn_done", cancelled: true } as SessionEvent);
    expect(asks(ended).map((i) => [i.ask.id, i.answered])).toEqual([["answered", [["egui 0.27"]]]]);
    expect(ended.items.some((i) => i.t === "approval")).toBe(false);
  });

  it("keeps an approval the same way, for the same reason", () => {
    const s = run([approving("apv-1"), rebuild([said("先写文件")])]);
    expect(s.items.filter((i) => i.t === "approval")).toHaveLength(1);
  });

  // The record is what a rebuild is for: what the rebuild brought back stays
  // where the kernel put it, and the open prompt lands after it.
  it("puts the rebuilt record back, with the question after it", () => {
    const s = run([asking("ask-1"), rebuild([said("一"), said("二")])]);
    expect(s.items.map((i) => i.t)).toEqual(["say", "say", "ask"]);
  });

  it("does not resurrect one that was already answered", () => {
    const first = run([asking("ask-1")]);
    const answered = reduce(first, { kind: "__decided", id: asks(first)[0].id, answers: [["egui 0.27"]] } as SessionEvent);
    const s = reduce(answered, rebuild([said("决定了")]));
    expect(asks(s), "an answered question belongs to the record, not to the wait").toHaveLength(0);
  });

  // EventSource reconnects on its own and the kernel replays the pending prompt
  // to whoever attaches, so the same question arrives more than once. Two cards
  // for one question are two answers the kernel will not both accept.
  it("shows one card per question however many times it is replayed", () => {
    const s = run([asking("ask-1"), asking("ask-1")]);
    expect(asks(s)).toHaveLength(1);
  });

  it("tells two different questions apart", () => {
    const s = run([asking("ask-1"), asking("ask-2")]);
    expect(asks(s)).toHaveLength(2);
  });

  // What seals a card the user did not click is the decision the kernel
  // recorded, which arrives on the same ordered stream as the request. It used
  // to be the /status projection, polled four times a second: a snapshot taken
  // before the prompt existed omits it exactly as a resolved one does, so a
  // card could be sealed within 250ms of arriving while the run stayed blocked
  // on it — buttons gone, and the line underneath claiming it was allowed.
  const receipt = (id: string, outcome: string): SessionEvent =>
    ({ kind: "notice", code: "decision_receipt", decisionReceipt: { id, kind: "tool", outcome } }) as SessionEvent;
  const answerable = (s: SessionState) =>
    s.items.filter((i): i is Extract<Item, { t: "approval" }> => i.t === "approval" && i.verdict === undefined);

  it("stays answerable until a decision is actually recorded", () => {
    const s = run([approving("apv-1"), approving("apv-2")]);
    expect(answerable(s).map((i) => i.a.id)).toEqual(["apv-1", "apv-2"]);
  });

  it("seals the one the receipt names, with what was decided", () => {
    const s = run([approving("apv-1"), approving("apv-2"), receipt("apv-1", "deny")]);
    expect(answerable(s).map((i) => i.a.id)).toEqual(["apv-2"]);
    const sealed = s.items.find((i) => i.t === "approval" && i.a.id === "apv-1") as Extract<Item, { t: "approval" }>;
    expect(sealed.verdict, "the outcome the kernel recorded, not a guess").toBe("deny");
    expect(s.items.some((i) => i.t === "notice"), "the audit receipt is not a chat message").toBe(false);
  });

  it("does not guess that a new receipt outcome was allowed", () => {
    const s = run([approving("apv-1"), receipt("apv-1", "future_outcome")]);
    expect((s.items[0] as Extract<Item, { t: "approval" }>).verdict).toBe("unknown");
  });

  it("does not present another window's answer as an unanswered question", () => {
    const s = run([asking("ask-1"), receipt("ask-1", "answered")]);
    const ask = s.items[0] as Extract<Item, { t: "ask" }>;
    expect(ask.answeredElsewhere).toBe(true);
    expect(ask.answered).toEqual([]);
  });

  // The receipt for the click this window made arrives right behind it. It must
  // not reopen or relabel a card the user already watched settle.
  it("leaves a card this window already sealed alone", () => {
    const clicked = run([approving("apv-1")]);
    const local = reduce(clicked, { kind: "__decided", id: clicked.items[0].id, verdict: "always" } as SessionEvent);
    const s = reduce(local, receipt("apv-1", "allow_session"));
    expect((s.items[0] as Extract<Item, { t: "approval" }>).verdict).toBe("always");
  });

  // The receipt can also overtake the answer's own reply. The card then reads
  // what this window sent, not "handled in another window".
  it("keeps this window's answer when the receipt overtook it", () => {
    const racing = run([asking("ask-1"), receipt("ask-1", "answered")]);
    const s = reduce(racing, { kind: "__decided", id: racing.items[0].id, answers: [["B"]] } as SessionEvent);
    const ask = s.items[0] as Extract<Item, { t: "ask" }>;
    expect(ask.answeredElsewhere).toBe(false);
    expect(ask.answered).toEqual([["B"]]);
  });
});

// A session's coverage is the kernel's answer, not this side's running guess:
// the record is not replayed on restore, so the fold has folded nothing yet.
describe("restoring what a session has been billed", () => {
  const totals = (over: Partial<Extract<SessionEvent, { kind: "__totals" }>> = {}): SessionEvent =>
    ({ kind: "__totals", hit: 10, miss: 5, cost: 0.02, ...over }) as SessionEvent;

  it("takes the coverage the kernel holds every round for", () => {
    const s = reduce(initialState, totals({ coverage: "partial", incompleteReason: "no_price" }));
    expect(s.metrics.coverage).toBe("partial");
    expect(s.metrics.incompleteReason).toBe("no_price");
    expect(s.metrics.cost).toBe(0.02);
  });

  // Replaced rather than joined, for the same reason the totals are: the read
  // belongs to whichever session this pane now holds.
  it("replaces what a previously held session had folded", () => {
    const held = reduce(initialState, totals({ coverage: "partial", incompleteReason: "no_price" }));
    const switched = reduce(held, totals({ coverage: "complete" }));
    expect(switched.metrics.coverage).toBe("complete");
    expect(switched.metrics.incompleteReason, "the reason belonged to the other session").toBe("");
  });

  // A kernel older than the field restores nothing rather than a guess: the
  // live fold takes over from the next round, which is all it can speak for.
  it("restores no coverage from a kernel that cannot say", () => {
    const s = reduce(initialState, totals({ coverage: undefined }));
    expect(s.metrics.coverage).toBe("none");
    expect(s.metrics.cost).toBe(0.02);
  });
});

// A turn can run one model and hand part of it to another. Without the turn's
// own name on its replies, a reader has only the composer's current setting,
// which is a different fact and moves on its own.
describe("which model wrote a reply", () => {
  const says = (st: SessionState) => st.items.filter((i): i is Extract<Item, { t: "say" }> => i.t === "say");

  it("names the turn's model on the replies inside it", () => {
    const st = run([
      { kind: "turn_started", authoredTurn: 1, msgIndex: 0, modelRef: "yyds/claude-opus-4.8" },
      { kind: "text", text: "on it" },
    ] as SessionEvent[]);
    expect(says(st)[0].model).toBe("yyds/claude-opus-4.8");
  });

  // The second model names itself, and must not be recorded as the turn's.
  it("keeps a second model's own name on what it wrote", () => {
    const st = run([
      { kind: "turn_started", authoredTurn: 1, msgIndex: 0, modelRef: "yyds/claude-opus-4.8" },
      { kind: "text", text: "planning", source: "planner", modelRef: "deepseek/deepseek-pro" },
    ] as SessionEvent[]);
    expect(says(st)[0].model).toBe("deepseek/deepseek-pro");
  });
});
