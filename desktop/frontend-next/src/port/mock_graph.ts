import type { ExecutionGraph, ExecutionGraphRead, GraphEdge, GraphNode, WireEvent } from "./wire";

/** The graph the script has published so far, folded the way the kernel answers
 *  its own authority. A mock run bootstraps from this, so the snapshot path is
 *  exercised here too rather than only against a live kernel. */
export function graphAt(log: WireEvent[]): ExecutionGraph {
  const nodes: GraphNode[] = [];
  const at = new Map<string, number>();
  const edges: GraphEdge[] = [];
  const drawn = new Set<string>();
  for (const ev of log) {
    for (const n of ev.graph?.nodes ?? []) {
      const i = at.get(n.id);
      if (i === undefined) {
        at.set(n.id, nodes.length);
        nodes.push(n);
      } else nodes[i] = { ...nodes[i], ...n };
    }
    for (const e of ev.graph?.edges ?? []) {
      const key = [e.from, e.to, e.kind].join("\u0000");
      if (drawn.has(key)) continue;
      drawn.add(key);
      edges.push(e);
    }
  }
  return { nodes, edges };
}

/** mockExecutionGraph answers the way the kernel's own authority does, so the
 *  desktop's bootstrap — snapshot first, deltas folded onto it — is the path a
 *  mock run takes too. A fixture that only ever streamed deltas would leave
 *  that path exercised on real hardware and nowhere else. */
export function mockExecutionGraph(log: WireEvent[], sessionId: string, watermark: number): ExecutionGraphRead {
  return { schemaVersion: 1, sessionId, watermark, graph: graphAt(log) };
}

/** MockHold is what a mock subscription keeps while its caller reads a
 *  snapshot. The real transport holds there for a reason a fixture shares: a
 *  frame delivered first would be folded into a state about to be replaced. */
export class MockExecutionHold {
  private held: WireEvent[] = [];
  private booting = false;

  begin() {
    this.booting = true;
  }

  take(ev: WireEvent): boolean {
    if (!this.booting) return false;
    this.held.push(ev);
    return true;
  }

  release(deliver: (ev: WireEvent) => void) {
    this.booting = false;
    const queued = this.held;
    this.held = [];
    for (const ev of queued) deliver(ev);
  }
}
