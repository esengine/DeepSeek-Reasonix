import { describe, expect, it } from "vitest";
import { fromHistory, initialState, reduce } from "./session";
import type { SessionEvent } from "./session";
import type { SessionState } from "./session_types";
import { toolFacts } from "../ui/panels/derive";

const run = (s: SessionState, ...evs: SessionEvent[]) => evs.reduce(reduce, s);
const facts = (s: SessionState) => toolFacts(s.executions);

const dispatch = (id: string, name: string, extra: Record<string, unknown> = {}): SessionEvent =>
  ({ kind: "tool_dispatch", tool: { id, name, readOnly: true, ...extra } } as SessionEvent);
const result = (id: string, name: string, extra: Record<string, unknown> = {}): SessionEvent =>
  ({ kind: "tool_result", tool: { id, name, readOnly: true, output: "ok", ...extra } } as SessionEvent);
const call = (id: string, name: string, extra: Record<string, unknown> = {}): SessionEvent[] =>
  [dispatch(id, name, extra), result(id, name, extra)];

// A transcript is free to fold, nest and replace the cards it draws. Each of
// these is one of the ways it does that, and none of them is a change to what
// ran. The counts are asserted against the executions, never against the items.
describe("a transcript projection cannot change what ran", () => {
  // foldLastRead case 2: two lookups are replaced by one ReadsCard.
  it("survives two lookups being folded into one card", () => {
    const s = run(initialState, ...call("a", "read_file"), ...call("b", "read_file"));
    expect(s.items.filter((i) => i.t === "tool").length).toBe(0);
    expect(s.items.filter((i) => i.t === "reads").length).toBe(1);
    expect(facts(s).tools).toBe(2);
  });

  // foldLastRead case 1: a third lookup joins the card that already exists.
  it("survives another lookup joining an existing card", () => {
    const s = run(initialState, ...call("a", "read_file"), ...call("b", "read_file"), ...call("c", "grep"));
    expect(s.items.filter((i) => i.t === "reads").length).toBe(1);
    expect(facts(s).tools).toBe(3);
  });

  // The regression this whole change exists for: the count never goes down.
  it("never counts down as cards collapse", () => {
    let s = initialState;
    const seen: number[] = [];
    for (const id of ["a", "b", "c", "d", "e"]) {
      for (const ev of call(id, "read_file")) {
        s = run(s, ev);
        seen.push(facts(s).tools);
      }
    }
    expect(seen).toEqual([...seen].sort((x, y) => x - y));
    expect(facts(s).tools).toBe(5);
  });

  // foldTool(parentId): a subagent's call is drawn inside the task that spawned
  // it and is never a top-level card at all.
  it("counts a subagent's call that is drawn inside its parent", () => {
    const s = run(initialState,
      ...call("p", "task"),
      ...call("k", "bash", { parentId: "p" }));
    expect(s.items.filter((i) => i.t === "tool").length).toBe(1);
    expect(facts(s).tools).toBe(2);
  });

  // dropTool: the one projection that deletes a card outright. The saved memory
  // replaces the row; the call still ran.
  it("counts a call whose card was replaced outright", () => {
    const s = run(initialState,
      dispatch("m", "remember"),
      result("m", "remember", { args: JSON.stringify({ name: "n", description: "d" }) }));
    expect(s.items.some((i) => i.t === "remember")).toBe(true);
    expect(s.items.filter((i) => i.t === "tool").length).toBe(0);
    expect(facts(s).tools).toBe(1);
  });

  // The general form of all four, and the gate a new collapse rule has to pass:
  // the same executions drawn two legal ways answer identically.
  it("answers the same under two different legal projections", () => {
    const folded = run(initialState, ...call("a", "read_file"), ...call("b", "read_file"));
    const apart = run(initialState, ...call("a", "read_file"), ...call("b", "bash"));
    expect(folded.items.filter((i) => i.t === "tool").length)
      .not.toBe(apart.items.filter((i) => i.t === "tool").length);
    expect(facts(folded).tools).toBe(facts(apart).tools);
  });
});

describe("what a call's outcome is, and when it has one", () => {
  it("has no outcome while it is still running", () => {
    const s = run(initialState, dispatch("a", "bash"));
    expect(s.executions["a"].failed).toBeUndefined();
    expect(facts(s).failed).toBe(0);
  });

  it("counts a failure that the card it was folded into no longer shows", () => {
    const s = run(initialState,
      ...call("a", "read_file"),
      dispatch("b", "read_file"),
      result("b", "read_file", { err: "no such file" }));
    expect(s.items.filter((i) => i.t === "tool").length).toBe(0);
    expect(facts(s).failed).toBe(1);
    expect(facts(s).tools).toBe(2);
  });

  // use_capability is the proxy most tools are reached through, so its own name
  // says nothing about who answered.
  it("classifies reach by what answered, not by the proxy", () => {
    const s = run(initialState, ...call("a", "use_capability", { resolvedName: "mcp__figma__get" }));
    expect(facts(s).external).toBe(1);
  });
});

describe("identity, not accumulation", () => {
  // Nothing here counts up. Reducing the same events again — a replay, a
  // reconnect that re-delivers, a rebuild — is the same fact restated.
  it("does not count the same execution twice when its events are replayed", () => {
    const evs = [...call("a", "bash"), ...call("b", "read_file")];
    const once = run(initialState, ...evs);
    const twice = run(once, ...evs);
    expect(facts(twice)).toEqual(facts(once));
  });

  it("says the same thing for dispatch, progress and result of one call", () => {
    const s = run(initialState,
      dispatch("a", "bash"),
      { kind: "tool_progress", tool: { id: "a", name: "bash", readOnly: true } } as SessionEvent,
      result("a", "bash"));
    expect(facts(s).tools).toBe(1);
  });

  // A call the wire gave no id has no identity to key on. The transcript cannot
  // merge one either — it draws the dispatch and the result as two cards — so
  // this is a bound of the wire, stated rather than papered over.
  it("does not record a call the wire gave no id", () => {
    const s = run(initialState,
      { kind: "tool_dispatch", tool: { name: "bash", readOnly: true } } as SessionEvent,
      { kind: "tool_result", tool: { name: "bash", readOnly: true, output: "ok" } } as SessionEvent);
    expect(facts(s).tools).toBe(0);
    expect(s.items.filter((i) => i.t === "tool").length).toBe(2);
  });
});

// A rebuild is the other way executions arrive. /history says a call ran and
// under what name; it carries no error field, so the outcome stays unknown
// rather than being read as success.
describe("what a rebuild can and cannot restore", () => {
  const rebuilt = () =>
    fromHistory([
      { role: "user", content: "go" },
      { role: "assistant", content: "", toolCalls: [
        { id: "h1", name: "read_file", arguments: "{}" },
        { id: "h2", name: "web_fetch", arguments: "{}" },
      ] },
      { role: "tool", content: "boom", toolCallId: "h2" },
    ]);

  it("restores that the calls ran, and what reached outside", () => {
    const f = toolFacts(rebuilt().executions);
    expect(f.tools).toBe(2);
    expect(f.external).toBe(1);
  });

  it("leaves the outcome unknown rather than calling it a success", () => {
    expect(rebuilt().executions["h2"].failed).toBeUndefined();
    expect(toolFacts(rebuilt().executions).failed).toBe(0);
  });

  // The restore has to hand the executions over with the transcript. Dropping
  // them left the next tool event reducing against nothing at all.
  it("keeps counting after a restore", () => {
    const r = rebuilt();
    let s = reduce(initialState, { kind: "__restore", ...r } as SessionEvent);
    expect(facts(s).tools).toBe(2);
    s = run(s, ...call("live", "bash"));
    expect(facts(s).tools).toBe(3);
  });
});
