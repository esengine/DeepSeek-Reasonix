import { describe, expect, it } from "vitest";
import { initialState, reduce } from "./session";
import type { SessionState } from "./session";
import type { WireEvent } from "../port/wire";
import { runState } from "../ui/decisions";

const ev = (e: Partial<WireEvent> & { kind: WireEvent["kind"] }) => e as WireEvent;
const feed = (s: SessionState, ...evs: WireEvent[]) => evs.reduce(reduce, s);
const ran = () => feed(initialState, ev({ kind: "turn_started" }), ev({ kind: "text", text: "ok" }));

// Exactly the call site's arithmetic, so a rule proved here is the rule the
// window applies.
const view = (s: SessionState) =>
  runState({ blocked: false, running: s.running, hasItems: s.items.length > 0, terminal: s.terminal });

describe("how a turn ended is a fact about this turn, not a label beside it", () => {
  // The counterfactual in both directions. The label the reducer writes is
  // presentation, and it is free to say anything without moving the verdict.
  it("does not read the label as a finish", () => {
    const s = { ...ran(), doing: "已完成", terminal: null };
    expect(view(s)).not.toBe("done");
  });

  it("finishes on the typed fact whatever the label says", () => {
    for (const doing of ["正在继续", "已中断", "Working", ""]) {
      const s: SessionState = { ...ran(), running: false, doing, terminal: { kind: "completed" } };
      expect(view(s), doing).toBe("done");
    }
  });

  // Only a delivery claims the tick. The label this replaced asked whether an
  // error was attached, so a turn that ended because its obligations were not
  // met read exactly like one that finished the work.
  it("keeps a turn that stopped short of its obligations off the tick", () => {
    const s = feed(ran(), ev({ kind: "turn_done", outcome: "final_readiness" }));
    expect(s.terminal).toEqual({ kind: "incomplete", outcome: "final_readiness" });
    expect(view(s)).toBe("halt");
  });

  it("tells a delivery, a failure and a stop apart", () => {
    expect(feed(ran(), ev({ kind: "turn_done" })).terminal).toEqual({ kind: "completed" });
    expect(feed(ran(), ev({ kind: "turn_done", err: "boom" })).terminal).toEqual({ kind: "failed", err: "boom" });
    // Cancellation carries an err of its own, so the flag is what separates a
    // stop from a dropped connection.
    expect(feed(ran(), ev({ kind: "turn_done", err: "context canceled", cancelled: true })).terminal)
      .toEqual({ kind: "cancelled" });
    expect(view(feed(ran(), ev({ kind: "turn_done" })))).toBe("done");
    expect(view(feed(ran(), ev({ kind: "turn_done", err: "boom" })))).toBe("halt");
  });

  // The rule that keeps this a state and not a history: without it the tick from
  // an hour ago would sit over work that is running now.
  it("forgets how the last turn ended when the next one starts", () => {
    const s = feed(ran(), ev({ kind: "turn_done" }), ev({ kind: "turn_started" }));
    expect(s.terminal).toBeNull();
    expect(view(s)).toBe("running");
  });

  // A job finishing behind the session says nothing about the turn in front of
  // it, in either direction.
  it("is not moved by anything happening in the background", () => {
    const done = feed(ran(), ev({ kind: "turn_done" }));
    const after = feed(done, ev({ kind: "inbox_changed" }), ev({ kind: "notice", text: "job done" } as never));
    expect(after.terminal).toEqual(done.terminal);

    const live = feed(initialState, ev({ kind: "turn_started" }), ev({ kind: "inbox_changed" }));
    expect(live.terminal).toBeNull();
  });

  // Pressing stop is a request; the turn is terminal when the kernel says so.
  it("does not call a turn over while it is still running", () => {
    const s = ran();
    expect(s.terminal).toBeNull();
    expect(view(s)).toBe("running");
  });
});
