import { describe, expect, it } from "vitest";
import { H, W, layout } from "./glayout";
import type { Lane } from "../state/graph";
import type { GraphEdge, GraphNode } from "../port/wire";

const node = (id: string): GraphNode => ({ id, kind: "worker", state: "completed", label: id });

const lane = (ranks: string[][], edges: GraphEdge[] = []): Lane => ({
  group: { id: "g", kind: "group", state: "completed", label: "fleet" },
  ranks: ranks.map((rank) => rank.map(node)),
  edges,
  reuse: {},
  blocked: {},
  ran: ranks.flat().length,
  reused: 0,
  queued: 0,
});

const depends = (from: string, to: string): GraphEdge => ({ from, to, kind: "depends" });

const yOf = (l: ReturnType<typeof layout>, id: string) => l.nodes.find((p) => p.node.id === id)!.y;
const xOf = (l: ReturnType<typeof layout>, id: string) => l.nodes.find((p) => p.node.id === id)!.x;

describe("arranging one lane", () => {
  it("reorders a column so its wires stop crossing", () => {
    // Arrival order puts a above b and x above y, so a→y and b→x cross. Nothing
    // about the run says they must: the rows were never decided, only inherited.
    const l = layout(lane([["a", "b"], ["x", "y"]], [depends("a", "y"), depends("b", "x")]));
    expect(yOf(l, "y")).toBeLessThan(yOf(l, "x"));
    // The column a wire lands in is the one its rank says, never a row order.
    expect(xOf(l, "x")).toBe(xOf(l, "y"));
    expect(xOf(l, "a")).toBeLessThan(xOf(l, "x"));
  });

  it("routes a wire that spans a column through it rather than over it", () => {
    const l = layout(
      lane([["a"], ["b"], ["c"]], [depends("a", "b"), depends("b", "c"), depends("a", "c")]),
    );
    const long = l.wires.find((w) => w.from === "a" && w.to === "c")!;
    // Start, one waypoint in the column it crosses, end. Without the waypoint
    // the wire is drawn straight over b's card and reads as pointing at b.
    expect(long.points).toHaveLength(3);
    expect(long.points[1].x).toBe(W + 64 + W / 2);
    const short = l.wires.find((w) => w.from === "a" && w.to === "b")!;
    expect(short.points).toHaveLength(2);
  });

  it("centres a short column against the tallest one", () => {
    const l = layout(lane([["a"], ["b", "c"]]));
    // Two cards plus the gap between them; the lone card sits at their middle.
    expect(l.h).toBe(H * 2 + 12);
    expect(yOf(l, "a")).toBe((l.h - H) / 2);
    expect(yOf(l, "b")).toBe(0);
  });

  it("draws the same lane the same way twice", () => {
    const twice = lane([["a", "b", "c"], ["x", "y"]], [depends("c", "x"), depends("a", "y"), depends("b", "y")]);
    expect(JSON.stringify(layout(twice))).toBe(JSON.stringify(layout(twice)));
  });

  it("folds ordering and delivery onto one wire", () => {
    const l = layout(
      lane([["a"], ["b"]], [depends("a", "b"), { from: "a", to: "b", kind: "context" }]),
    );
    // Two facts about one pair, not two relationships between two nodes.
    expect(l.wires).toHaveLength(1);
    expect(l.wires[0].carried).toBe(true);
  });

  it("drops a wire naming a node this lane does not hold", () => {
    const l = layout(lane([["a"], ["b"]], [depends("a", "b"), depends("sa-old", "a")]));
    expect(l.wires.map((w) => w.from)).toEqual(["a"]);
  });

  it("sizes a lane holding one node to that node", () => {
    const l = layout(lane([["a"]]));
    expect([l.w, l.h]).toEqual([W, H]);
  });
});
