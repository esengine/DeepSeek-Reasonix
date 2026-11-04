import { describe, expect, it } from "vitest";
import { fromHistory, initialState, reduce, type Item, type SessionEvent } from "./session";
import { appendText, foldMessage } from "./say";

// The kinds internal/frontend/serve/wirelog.go keeps. Everything else is dropped from
// the record on purpose — streamed deltas are one frame per chunk and say
// nothing the settled frame does not.
const KEPT = new Set(["turn_started", "message", "tool_dispatch", "tool_result", "turn_done"]);

const ev = (kind: string, rest: Record<string, unknown> = {}) => ({ kind, ...rest }) as SessionEvent;
const tool = (id: string) => ({ id, name: "probe_read", readOnly: true, output: "probe file contents" });

// One two-model turn as the kernel emits it: the planner reasons, calls a tool
// and writes its plan; the executor does the same and answers. Transcribed from
// a recorded plan_and_execute run, frame for frame.
const LIVE: SessionEvent[] = [
  ev("turn_started", { authoredTurn: 1, msgIndex: 1 }),
  ev("phase", { text: "planner · planning" }),
  ev("reasoning", { text: "considering probe_read" }),
  ev("message", { text: "", reasoning: "considering probe_read" }),
  ev("tool_dispatch", { tool: tool("planner-probe") }),
  ev("tool_result", { tool: tool("planner-probe") }),
  ev("reasoning", { text: "the shape is clear" }),
  ev("text", { text: "1. change the thing" }),
  ev("message", { text: "1. change the thing", reasoning: "the shape is clear" }),
  ev("phase", { text: "exec · executing" }),
  ev("reasoning", { text: "considering probe_read" }),
  ev("message", { text: "", reasoning: "considering probe_read" }),
  ev("tool_dispatch", { tool: tool("exec-probe") }),
  ev("tool_result", { tool: tool("exec-probe") }),
  ev("reasoning", { text: "that settles it" }),
  ev("text", { text: "done: the thing is changed" }),
  ev("message", { text: "done: the thing is changed", reasoning: "that settles it" }),
  ev("turn_done"),
];

const says = (items: Item[]) =>
  items.filter((i) => i.t === "say").map((i) => ({ text: i.text, reasoning: i.reasoning, done: i.done }));

describe("the settled message frame is enough to rebuild the card", () => {
  // The record keeps no streamed deltas, so a reopened session has only the
  // settled frames. If those cannot build a card, every answer in the
  // transcript — the planner's especially, which is in no other durable place —
  // comes back missing.
  it("rebuilds the same cards from the frames the record keeps", () => {
    const live = says(LIVE.reduce(reduce, initialState).items);
    const durable = says(LIVE.filter((e) => KEPT.has((e as { kind: string }).kind)).reduce(reduce, initialState).items);
    expect(durable).toEqual(live);
    expect(live).toHaveLength(4);
  });

  // The frame is the settled text, with the host's markers already out of it
  // and any reasoning an extension replaced. A stream that lost a chunk is
  // repaired by it rather than left short.
  it("takes the settled text over what the stream managed to deliver", () => {
    const dropped = [
      ev("turn_started"),
      ev("text", { text: "done: the thi" }),
      ev("message", { text: "done: the thing is changed", reasoning: "that settles it" }),
    ];
    expect(says(dropped.reduce(reduce, initialState).items)).toEqual([
      { text: "done: the thing is changed", reasoning: "that settles it", done: true },
    ]);
  });

  // An empty field is not content: a tool-call round settles with reasoning
  // only, and it must not wipe what the stream already showed.
  it("never overwrites a card with an empty field", () => {
    const partial = [
      ev("turn_started"),
      ev("text", { text: "half an answer" }),
      ev("message", { text: "", reasoning: "thinking about it" }),
    ];
    expect(says(partial.reduce(reduce, initialState).items)).toEqual([
      { text: "half an answer", reasoning: "thinking about it", done: true },
    ]);
  });
});

// The kernel times the thought and keeps it with the turn; this window's own
// clock is only a stand-in for turns recorded before it did.
describe("thinking time", () => {
  it("takes the kernel's measure over the window's own", () => {
    let items = appendText([], "weighing", "reasoning");
    items = foldMessage(items, { kind: "message", text: "answer", reasoning: "weighing", thoughtMs: 65800 });
    expect((items[0] as Extract<Item, { t: "say" }>).thoughtMs).toBe(65800);
  });

  it("keeps it when a transcript is reopened", () => {
    const { items } = fromHistory([
      { role: "user", content: "q", msgIndex: 0 },
      { role: "assistant", content: "a", reasoning: "r", thoughtMs: 4200, msgIndex: 1 },
    ]);
    const say = items.find((i) => i.t === "say") as Extract<Item, { t: "say" }>;
    expect(say.thoughtMs).toBe(4200);
  });
});

describe("a line typed while the model is writing", () => {
  const run = (events: SessionEvent[]) => events.reduce(reduce, initialState);
  const says = (items: Item[]) => items.filter((i) => i.t === "say") as Extract<Item, { t: "say" }>[];

  it("does not cut the thought that is still arriving into two blocks", () => {
    const s = run([
      ev("turn_started", { authoredTurn: 1, msgIndex: 1 }),
      ev("reasoning", { text: "first half, " }),
      ev("__user", { text: "also check the tests", pending: true }),
      ev("reasoning", { text: "second half" }),
      ev("text", { text: "answer" }),
    ]);
    const blocks = says(s.items);
    expect(blocks).toHaveLength(1);
    expect(blocks[0].reasoning).toBe("first half, second half");
    expect(blocks[0].text).toBe("answer");
    expect(s.items.map((i) => i.t)).toEqual(["say", "user"]);
  });

  it("still starts a new message after the line has been delivered and the earlier one settled", () => {
    const s = run([
      ev("turn_started", { authoredTurn: 1, msgIndex: 1 }),
      ev("reasoning", { text: "before" }),
      ev("message", { text: "", reasoning: "before" }),
      ev("__user", { text: "steer me", pending: true }),
      ev("steer", { text: "steer me" }),
      ev("reasoning", { text: "after" }),
    ]);
    expect(says(s.items)).toHaveLength(2);
  });
});

describe("where a thought ends", () => {
  const run = (...events: SessionEvent[]) => events.reduce(reduce, initialState);
  const say = (s: ReturnType<typeof run>) => s.items.find((i) => i.t === "say") as Extract<Item, { t: "say" }>;

  it("stops the clock when the model turns to a call, so only the call moves", () => {
    const s = run(ev("turn_started", { authoredTurn: 1, msgIndex: 1 }), ev("reasoning", { text: "look first" }), ev("message", { text: "", reasoning: "look first" }), ev("tool_dispatch", { tool: tool("t1") }));
    expect(say(s).done).toBe(true);
    expect(say(s).thoughtMs).toBeTypeOf("number");
  });

  it("says a thought was cut short when the turn is stopped or fails while it is still arriving", () => {
    const thinking = [ev("turn_started", { authoredTurn: 1, msgIndex: 1 }), ev("reasoning", { text: "half a thought" })];
    expect(say(run(...thinking, ev("turn_done", { cancelled: true }))).cut).toBe("stopped");
    expect(say(run(...thinking, ev("turn_done", { err: "provider down" }))).cut).toBe("failed");
  });

  it("does not call a finished thought cut short when the turn stops later", () => {
    const s = run(ev("turn_started", { authoredTurn: 1, msgIndex: 1 }), ev("reasoning", { text: "done thinking" }), ev("text", { text: "answer" }), ev("turn_done", { cancelled: true }));
    expect(say(s).cut).toBeUndefined();
  });
});
