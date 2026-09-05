import { describe, expect, it } from "vitest";
import type { Decision, SessionStatus } from "../port/session";
import type { TurnTerminal } from "../state/session_types";
import { hasPendingDecision, runState } from "./decisions";

const DONE: TurnTerminal = { kind: "completed" };

const KINDS: Decision["kind"][] = ["tool_approval", "plan_approval", "recovery_approval", "ask"];

const status = (decisions?: Decision[]): SessionStatus =>
  ({ decisions } as unknown as SessionStatus);

const waiting = (kind: Decision["kind"]): Decision => ({ id: `d-${kind}`, kind });

// Labels this window has written, or could write, in the place the verdict used
// to be read from. The first two are the literals that owned it; the rest are
// what the same field says in another language or after a rewrite.
const LABELS = ["等你批准", "等你决定", "Waiting for approval", "正在继续工作", "foo", ""];

describe("pending decisions, not localized doing text, own blocked state", () => {
  // The counterfactual that matters most: the old wording is still on screen and
  // must no longer carry any authority.
  it("does not read the old label as a reason to wait", () => {
    for (const label of LABELS) {
      const blocked = hasPendingDecision(status([]));
      const run = runState({ blocked, running: true, hasItems: true, terminal: label === "已完成" ? DONE : null });
      expect(blocked, `label ${JSON.stringify(label)}`).toBe(false);
      expect(run, `label ${JSON.stringify(label)}`).toBe("running");
    }
  });

  // And the other half: a typed decision decides on its own, whatever the label
  // beside it happens to say — including a label that reads like progress.
  it("waits on a typed decision whatever the label says", () => {
    for (const kind of KINDS) {
      for (const label of [...LABELS, "已完成"]) {
        const blocked = hasPendingDecision(status([waiting(kind)]));
        const run = runState({ blocked, running: true, hasItems: true, terminal: label === "已完成" ? DONE : null });
        expect(blocked, `${kind} / ${JSON.stringify(label)}`).toBe(true);
        expect(run, `${kind} / ${JSON.stringify(label)}`).toBe("halt");
      }
    }
  });

  // All four are answered by different calls and all four need a person, so the
  // condition is the list's length. A test that named some of them would fail to
  // notice `decisions.some(d => d.kind !== "ask")`.
  it("counts every kind the kernel can be waiting on", () => {
    for (const kind of KINDS) expect(hasPendingDecision(status([waiting(kind)])), kind).toBe(true);
    expect(hasPendingDecision(status(KINDS.map(waiting)))).toBe(true);
  });

  // The field is optional and rides an interface assertion on the kernel side.
  // Absent has to mean "cannot say anyone is waiting", never "fall back to the
  // label" — a silent heuristic would put presentation back in charge with
  // nothing on screen or in a test to say so.
  it("says no one is waiting when the kernel did not answer", () => {
    expect(hasPendingDecision(status())).toBe(false);
    expect(hasPendingDecision(status([]))).toBe(false);
    expect(hasPendingDecision(null)).toBe(false);
    expect(hasPendingDecision(undefined)).toBe(false);
  });
});

describe("runState", () => {
  // Facts in, verdict out. Nothing here can be told what the screen says.
  it("puts waiting on a person ahead of every other state", () => {
    expect(runState({ blocked: true, running: true, hasItems: true, terminal: DONE })).toBe("halt");
    expect(runState({ blocked: true, running: false, hasItems: false, terminal: null })).toBe("halt");
  });

  it("reads an empty session as idle and a live one as running", () => {
    expect(runState({ blocked: false, running: false, hasItems: false, terminal: null })).toBe("idle");
    expect(runState({ blocked: false, running: true, hasItems: false, terminal: null })).toBe("running");
  });

  it("separates a turn that ended from one that stopped part way", () => {
    expect(runState({ blocked: false, running: false, hasItems: true, terminal: DONE })).toBe("done");
    expect(runState({ blocked: false, running: false, hasItems: true, terminal: null })).toBe("halt");
  });
});
