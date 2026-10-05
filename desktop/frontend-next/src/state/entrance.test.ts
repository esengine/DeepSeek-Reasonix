import { describe, expect, it } from "vitest";
import { initialState, reduce } from "./session";
import type { SessionState } from "./session_types";
import type { SessionEvent } from "./session";

// Motion describes a state transition, never an element's lifetime. The one
// transition a card's entrance is about is a fact arriving, so the projection
// — not the DOM, not a provenance label on the item — is what says whether one
// is still owed. These hold that: a fact earns its entrance once, and nothing
// that happens to the same fact afterwards earns it again.
const run = (s: SessionState, ...evs: SessionEvent[]) => evs.reduce(reduce, s);
const say = (text: string): SessionEvent => ({ kind: "text", text } as SessionEvent);
const dispatched = (id: string, name = "read_file"): SessionEvent =>
  ({ kind: "tool_dispatch", tool: { id, name, readOnly: true } } as SessionEvent);
const resulted = (id: string, name = "read_file"): SessionEvent =>
  ({ kind: "tool_result", tool: { id, name, readOnly: true, output: "ok" } } as SessionEvent);

describe("which cards owe an entrance", () => {
  it("gives a card that just arrived exactly one", () => {
    const s = run(initialState, { kind: "__user", text: "hi", pending: false });
    expect(s.entranceOwed.length).toBe(1);
    expect(s.items.map((i: { id: string }) => i.id)).toEqual(s.entranceOwed);
  });

  it("owes nothing for a transcript that is being restored", () => {
    const live = run(initialState, { kind: "__user", text: "hi", pending: false });
    const cold = run(live, { kind: "__restore", items: [
      { t: "user", id: "h1", text: "a" },
      { t: "say", id: "h2", text: "b", done: true },
    ], plan: [], executions: {} } as SessionEvent);
    expect(cold.items.length).toBe(2);
    expect(cold.entranceOwed).toEqual([]);
  });

  // The debt is carried, not recomputed: several events can land between two
  // renders, and the card that was never drawn still owes its entrance.
  it("keeps what a render has not drawn yet", () => {
    const s = run(initialState,
      { kind: "__user", text: "one", pending: false },
      { kind: "__user", text: "two", pending: false });
    expect(s.entranceOwed.length).toBe(2);
  });

  it("spends only what the transcript says it drew", () => {
    const s = run(initialState,
      { kind: "__user", text: "one", pending: false },
      { kind: "__user", text: "two", pending: false });
    const after = run(s, { kind: "__entered", ids: [s.entranceOwed[0]] });
    expect(after.entranceOwed).toEqual([s.entranceOwed[1]]);
  });

  // The same card, over and over. A streamed answer rewrites one item two
  // hundred times and a call goes dispatch → result on one row; none of that
  // is a new fact.
  it("does not let a streaming answer earn it again", () => {
    let s = run(initialState, say("Hel"));
    const first = s.entranceOwed;
    expect(first.length).toBe(1);
    s = run(s, { kind: "__entered", ids: first });
    s = run(s, say("lo"), say(" there"));
    expect(s.entranceOwed).toEqual([]);
    expect(s.items.length).toBe(1);
  });

  it("does not let a call earn it again when it settles", () => {
    let s = run(initialState, dispatched("t1"));
    expect(s.entranceOwed.length).toBe(1);
    s = run(s, { kind: "__entered", ids: s.entranceOwed });
    s = run(s, resulted("t1"));
    expect(s.entranceOwed).toEqual([]);
    expect(s.items.length).toBe(1);
  });

  // Restoring in the middle of a session is still a restore: switching to
  // another conversation must not replay its whole history as if it were
  // arriving now.
  it("owes nothing when a live pane is handed a different transcript", () => {
    let s = run(initialState, { kind: "__user", text: "hi", pending: false });
    s = run(s, { kind: "__entered", ids: s.entranceOwed });
    s = run(s, { kind: "__restore", items: [
      { t: "user", id: "h9", text: "other" },
      { t: "say", id: "h10", text: "answer", done: true },
    ], plan: [], executions: {} } as SessionEvent);
    expect(s.entranceOwed).toEqual([]);
  });

  // And a fact arriving after that restore is still a new fact.
  it("still lets the next real arrival in", () => {
    let s = run(initialState, { kind: "__restore", items: [{ t: "user", id: "h1", text: "a" }], plan: [], executions: {} } as SessionEvent);
    s = run(s, { kind: "__user", text: "now", pending: false });
    expect(s.entranceOwed.length).toBe(1);
  });
});
