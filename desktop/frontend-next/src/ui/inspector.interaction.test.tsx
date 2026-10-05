// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import type { ComponentProps } from "react";
import "./testkit";
import { Metrics } from "./Metrics";
import { posture } from "./decisions";
import { MockPort } from "../port/mock";
import type { AgentPort, JobEntry } from "../port/port";

afterEach(cleanup);

const METRICS = {
  hit: 10, miss: 2, out: 100, bySource: {}, cost: 1.5, currency: "¥",
  prefixHash: "", prefixChanged: false, prefixReasons: [], bodyChanged: false,
  carriedMessages: 3, toolSchema: 1, estimated: false, alt: null, turn: 1, rounds: [],
} as never;

const JOB = { id: "j1", name: "bg", state: "running" } as unknown as JobEntry;

function draw(at: "working" | "review", over: Record<string, unknown> = {}, port?: AgentPort) {
  const props = {
    port: port ?? (new MockPort() as unknown as AgentPort),
    metrics: METRICS,
    tasks: [], changes: [{ path: "src/a.ts", added: 4, removed: 1, edits: 1 }],
    stats: { tools: 3, external: 1, failed: 0, waiting: 0 },
    jobs: [JOB], mcp: [], rate: 12, done: at === "review", posture: at,
    plan: [{ text: "step one", done: false }],
    wallet: { kind: "absent" } as never, account: "", onRefreshWallet: () => {},
    tree: null,
    ctx: { used: 24800, window: 128000, system: 5200, tools: 7400, user: 1800, reply: 3100, output: 7300 }, onCtx: () => {}, yolo: false, onSettings: () => {},
    panels: [], views: [], onExtInvoke: () => {}, onMoveSurface: () => {},
    ...over,
  } as unknown as ComponentProps<typeof Metrics>;
  const view = render(<Metrics {...props} />);
  const order = () => [...document.querySelectorAll("[data-b]")].map((el) => el.getAttribute("data-b"));
  return { view, order, props };
}

const before = (list: (string | null)[], a: string, b: string) => list.indexOf(a) < list.indexOf(b);

describe("what the inspector puts first", () => {
  it("leads with what the turn is doing while it is still going", () => {
    const { order } = draw("working");
    const at = order();
    expect(at[0]).toBe("ctx");
    expect(before(at, "ctx", "files")).toBe(true);
    expect(before(at, "runtime", "files")).toBe(true);
    // A plan is where it is now and what is next, not only an audit trail.
    expect(before(at, "plan", "cost")).toBe(true);
  });

  it("leads with what the turn left behind once it is over", () => {
    const at = draw("review").order();
    expect(at[0]).toBe("files");
    expect(before(at, "files", "ctx")).toBe(true);
    expect(before(at, "jobs", "ctx")).toBe(true);
    // How full the window is matters while there is a turn to fit in it, and
    // is forensic once there is not — so it drops below what the turn cost.
    expect(before(at, "cost", "ctx")).toBe(true);
  });

  // Order is a table, not a race. A panel that has just acquired something
  // must not jump the queue while the reader is watching another one.
  it("does not let a panel with something to say move itself up", () => {
    const quiet = draw("working").order();
    cleanup();
    const busy = draw("working", { changes: [{ path: "a.ts", added: 1, removed: 0 }], jobs: [JOB, JOB] }).order();
    expect(busy.indexOf("ctx")).toBe(0);
    expect(before(busy, "ctx", "files")).toBe(true);
    expect(quiet.filter((id) => busy.includes(id))).toEqual(quiet.filter((id) => busy.includes(id)));
  });

  // Presence is still each panel's own contract: composing an order must not
  // conjure a block to fill a slot in it.
  it("draws no block for a panel with nothing to say", () => {
    const at = draw("review", { jobs: [], plan: [], changes: [] }).order();
    expect(at).not.toContain("plan");
    // The same rule reached the file block last: review posture puts it first,
    // so an unchanged tree led the rail with a sentence about nothing.
    expect(at).not.toContain("files");
    // Jobs used to be the deliberate other case, reporting zero so the order
    // would not move when the last task finished. A block whose whole content
    // is "there are none" is what the salience pass took out, so it is now on
    // the same contract as the rest. What composing an order still may not do
    // is conjure one, or drop a block that does have something.
    expect(at).not.toContain("jobs");
    expect(at).toContain("cost");
  });

  // The one that matters most: changing posture is not a change to any
  // panel's lifetime. Two branches of JSX would tear these down and rebuild
  // them, losing whatever was open inside and asking again for what they read.
  it("moves panels without rebuilding them", () => {
    const { view, props } = draw("working");
    const node = document.querySelector('[data-b="cache"]') as HTMLElement;
    expect(node).toBeTruthy();
    node.dataset.probe = "same-element";
    view.rerender(<Metrics {...props} posture="review" done />);
    const after = document.querySelector('[data-b="cache"]') as HTMLElement;
    expect(after.dataset.probe, "the panel was torn down and built again").toBe("same-element");
    expect(after).toBe(node);
  });

  it("asks the kernel for nothing when it reorders", () => {
    const calls: string[] = [];
    const real = new MockPort();
    const watched = new Proxy(real, {
      get: (own, key: string) => {
        const v = (own as unknown as Record<string, unknown>)[key];
        if (typeof v !== "function") return v;
        return (...a: unknown[]) => { calls.push(key); return (v as (...x: unknown[]) => unknown).apply(own, a); };
      },
    }) as unknown as AgentPort;
    const { view, props } = draw("working", {}, watched);
    const spent = calls.length;
    view.rerender(<Metrics {...props} posture="review" done />);
    expect(calls.length).toBe(spent);
  });

  // Mcp speaks only when a server has failed or has never been answered for,
  // which is already "diagnose when there is something to diagnose". Ordering
  // it last is all this cut does with it: putting it behind a drawer would add
  // an empty one to every healthy rail and shut the one that is not.
  it("leaves the machine's own facts to their own contract, last", () => {
    const quiet = draw("working").order();
    expect(quiet).not.toContain("mcp");
    cleanup();
    const loud = draw("working", { mcp: [{ name: "codegraph", state: "failed" } as never] }).order();
    expect(loud).toContain("mcp");
    expect(loud.indexOf("mcp")).toBe(loud.length - 1);
    expect(document.querySelector("details.diag")).toBeNull();
  });
});

// Posture comes off the run, not off what the panels hold — and "halt" answers
// two questions, so it cannot be mapped on its own.
describe("which reading the rail is for", () => {
  it("stays on working while a turn is open", () => {
    expect(posture("running", false)).toBe("working");
    expect(posture("halt", true)).toBe("working");
  });

  it("turns to review once there is no turn to follow", () => {
    expect(posture("done", false)).toBe("review");
    expect(posture("halt", false)).toBe("review");
    expect(posture("idle", false)).toBe("review");
  });
});

// The task view draws the same steps at full width. Two readings of one list
// on one screen is not two sources of truth, but it is two hands: numbered
// with a cursor in the rail, dotted with a label in the view.
describe("the plan does not stand twice", () => {
  it("keeps the rail's copy while the conversation is in front", () => {
    const { order } = draw("working");
    expect(order()).toContain("plan");
  });

  it("stands the rail's copy down while the task view holds it", () => {
    const { order } = draw("working", { planShownElsewhere: true });
    expect(order()).not.toContain("plan");
  });

  it("is a question about this panel, not about the order", () => {
    const { order } = draw("working", { planShownElsewhere: true });
    // Everything else the posture asked for is still there, in its own place.
    expect(order()).toContain("ctx");
    expect(before(order(), "ctx", "cost")).toBe(true);
  });
});
