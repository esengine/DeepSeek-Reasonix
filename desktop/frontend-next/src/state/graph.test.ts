import { describe, expect, it } from "vitest";
import { foldDelta, graphOf, initialGraph, lanesOf, type GraphState } from "./graph";
import type { GraphDelta } from "../port/wire";

const run = (deltas: GraphDelta[]): GraphState => deltas.reduce<GraphState>(foldDelta, initialGraph);

const group = (id: string, label = "fleet(2)") => ({ id, kind: "group" as const, state: "running" as const, label });
const worker = (id: string, parentId: string, label: string) => ({
  id,
  parentId,
  kind: "worker" as const,
  state: "pending" as const,
  label,
});
const depends = (from: string, to: string) => ({ from, to, kind: "depends" as const });
const spawn = (from: string, to: string) => ({ from, to, kind: "spawn" as const });

describe("folding the published graph", () => {
  it("updates a node in place instead of forking it", () => {
    const s = run([
      { nodes: [worker("a", "g", "survey")] },
      { nodes: [{ id: "a", state: "completed", ref: "sa-1" }] },
    ]);
    expect(s.nodes).toHaveLength(1);
    // The outcome lands, and the identity declared when the node opened stays:
    // a producer sends what it just learned, not the whole node again.
    expect(s.nodes[0]).toMatchObject({ state: "completed", ref: "sa-1", label: "survey", kind: "worker" });
  });

  it("keeps depends and context apart on the same pair", () => {
    const s = run([
      { edges: [depends("a", "b"), { from: "a", to: "b", kind: "context" }, depends("a", "b")] },
    ]);
    expect(s.edges).toHaveLength(2);
    expect(s.edges.map((e) => e.kind)).toEqual(["depends", "context"]);
  });

  it("drops records it cannot draw", () => {
    const s = run([
      {
        nodes: [{ id: "", state: "running" }],
        edges: [{ from: "a", to: "a", kind: "depends" }, { from: "", to: "b", kind: "depends" }],
      },
    ]);
    expect(s).toBe(initialGraph);
  });

  // The kernel keeps the wait cause once a node starts running, the way it keeps
  // the stamps, and this fold has to agree: a reader who arrives after the run
  // started must still find what held it back.
  it("keeps what a node waited on after it starts running", () => {
    const s = run([
      { nodes: [worker("a", "g", "survey")] },
      { nodes: [{ id: "a", state: "queued", wait: "claim", queuedAt: 1000 }] },
      { nodes: [{ id: "a", state: "running", startedAt: 1500 }] },
    ]);
    expect(s.nodes[0]).toMatchObject({ state: "running", wait: "claim", queuedAt: 1000, startedAt: 1500 });
  });

  // The authority is indexed, not folded onto what was there: a reader that
  // merged the two would keep a node the rebuild no longer justifies.
  it("takes the kernel's own fold as the whole picture", () => {
    const s = graphOf({ nodes: [worker("c", "g", "third")], edges: [depends("a", "c"), depends("a", "c")] });
    expect(s.nodes.map((n) => n.id)).toEqual(["c"]);
    expect(s.edges).toHaveLength(1);
    expect(lanesOf(graphOf({ nodes: [] })).length).toBe(0);
  });
});

describe("arranging a fan-out for reading", () => {
  it("puts unordered work in one column and a chain across columns", () => {
    const s = run([
      {
        nodes: [group("g", "fleet(3)"), worker("g/1", "g", "survey"), worker("g/2", "g", "rewrite"), worker("g/3", "g", "aside")],
        edges: [spawn("g", "g/1"), spawn("g", "g/2"), spawn("g", "g/3"), depends("g/1", "g/2")],
      },
    ]);
    const [lane] = lanesOf(s);
    expect(lane.ranks).toHaveLength(2);
    // survey and aside wait for nothing, so they share the first column: a wide
    // column is what "these ran at the same time" looks like.
    expect(lane.ranks[0].map((n) => n.id)).toEqual(["g/1", "g/3"]);
    expect(lane.ranks[1].map((n) => n.id)).toEqual(["g/2"]);
  });

  it("ranks a diamond by its longest chain", () => {
    const s = run([
      {
        nodes: [group("g"), ...["g/1", "g/2", "g/3", "g/4"].map((id) => worker(id, "g", id))],
        edges: [depends("g/1", "g/2"), depends("g/2", "g/4"), depends("g/1", "g/3"), depends("g/3", "g/4")],
      },
    ]);
    const [lane] = lanesOf(s);
    expect(lane.ranks.map((r) => r.map((n) => n.id))).toEqual([["g/1"], ["g/2", "g/3"], ["g/4"]]);
  });

  it("lays out rather than hanging on a graph that arrived malformed", () => {
    const s = run([
      { nodes: [group("g"), worker("g/1", "g", "a"), worker("g/2", "g", "b")], edges: [depends("g/1", "g/2"), depends("g/2", "g/1")] },
    ]);
    expect(() => lanesOf(s)).not.toThrow();
    expect(lanesOf(s)[0].ranks.flat()).toHaveLength(2);
  });

  it("says what a waiting node is waiting for", () => {
    const s = run([
      {
        nodes: [group("g"), worker("g/1", "g", "survey"), worker("g/2", "g", "rewrite")],
        edges: [depends("g/1", "g/2")],
      },
    ]);
    // Nothing has answered yet: the dependent is blocked, and the panel can name
    // the node it is blocked on rather than showing it as merely not started.
    expect(lanesOf(s)[0].blocked).toEqual({ "g/2": ["g/1"] });

    const after = foldDelta(s, { nodes: [{ id: "g/1", state: "completed" }] });
    expect(lanesOf(after)[0].blocked).toEqual({});
  });

  it("names the finished work an adopted node reused", () => {
    const s = run([
      {
        nodes: [
          group("g"),
          { ...worker("g/1", "g", "survey"), state: "adopted" as const },
          worker("g/2", "g", "rewrite"),
          { id: "sa-old", kind: "external" as const, state: "completed" as const, ref: "sa-old" },
        ],
        edges: [{ from: "sa-old", to: "g/1", kind: "adopt" as const }, depends("g/1", "g/2")],
      },
    ]);
    const [lane] = lanesOf(s);
    expect(lane.reuse).toEqual({ "g/1": "sa-old" });
    expect(lane.reused).toBe(1);
    expect(lane.ran).toBe(1);
    // The external source is not a member of the lane; it is the chip on the
    // node that reused it, so it must not take a column of its own.
    expect(lane.ranks.flat().map((n) => n.id)).toEqual(["g/1", "g/2"]);
  });

  it("counts what the concurrency ceiling is holding back, apart from what the graph is", () => {
    const s = run([
      {
        nodes: [group("g", "fleet(3)"), worker("g/1", "g", "survey"), worker("g/2", "g", "rewrite"), worker("g/3", "g", "aside")],
        edges: [depends("g/1", "g/2")],
      },
      // g/1 got a slot, g/3 did not, g/2 is still waiting on its dependency.
      { nodes: [{ id: "g/1", state: "queued", queuedAt: 100 }, { id: "g/3", state: "queued", queuedAt: 100 }] },
      { nodes: [{ id: "g/1", state: "running", startedAt: 140 }] },
    ]);
    const [lane] = lanesOf(s);
    // Two different waits, and only one of them is answered by raising the cap.
    expect(lane.queued).toBe(1);
    expect(lane.blocked).toEqual({ "g/2": ["g/1"] });
  });

  it("keeps a stamp an update is silent about", () => {
    const s = run([
      { nodes: [worker("g/1", "g", "survey")] },
      { nodes: [{ id: "g/1", state: "queued", queuedAt: 100 }] },
      { nodes: [{ id: "g/1", state: "running", startedAt: 140 }] },
      { nodes: [{ id: "g/1", state: "completed", endedAt: 900 }] },
    ]);
    // Each delta carries one stamp. Folding them must leave all three standing,
    // or the span the graph draws is only ever the last one published.
    expect(s.nodes[0]).toMatchObject({ state: "completed", queuedAt: 100, startedAt: 140, endedAt: 900 });
  });

  it("keeps two fan-outs in separate lanes", () => {
    const s = run([
      { nodes: [group("a"), worker("a/1", "a", "one"), group("b"), worker("b/1", "b", "two")] },
    ]);
    expect(lanesOf(s).map((l) => l.group.id)).toEqual(["a", "b"]);
    expect(lanesOf(s)[1].ranks.flat().map((n) => n.id)).toEqual(["b/1"]);
  });
});
