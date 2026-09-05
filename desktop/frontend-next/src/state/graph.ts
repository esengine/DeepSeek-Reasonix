import type { ExecutionGraph, GraphDelta, GraphEdge, GraphNode } from "../port/wire";

// The run graph, folded the way internal/agentgraph folds it. The kernel is the
// only thing that knows a dependency from a delivered answer, so this reducer
// reads what it published rather than re-deriving structure from id prefixes —
// which is what the panel used to do, and why it could only ever draw a list.
export interface GraphState {
  nodes: GraphNode[];
  edges: GraphEdge[];
  // Where each node sits in `nodes`, and every edge already held. Carried with
  // the arrays rather than rebuilt on each fold: the graph is the one piece of
  // session state that only ever grows, so scanning it per delta made the cost
  // of a delta the size of the whole run — the thing perf/README forbids.
  at: Map<string, number>;
  seen: Set<string>;
}

export const initialGraph: GraphState = { nodes: [], edges: [], at: new Map(), seen: new Set() };

// A later record of the same node updates the one already there. Producers send
// an outcome once a node settles, not the whole node, so anything the update is
// silent about is kept rather than blanked.
function merge(old: GraphNode, update: GraphNode): GraphNode {
  return {
    ...old,
    ...update,
    parentId: update.parentId || old.parentId,
    kind: update.kind || old.kind,
    state: update.state || old.state,
    label: update.label || old.label,
    profile: update.profile || old.profile,
    model: update.model || old.model,
    effort: update.effort || old.effort,
    ref: update.ref || old.ref,
    err: update.err || old.err,
    grant: update.grant || old.grant,
    wait: update.wait || old.wait,
    queuedAt: update.queuedAt || old.queuedAt,
    startedAt: update.startedAt || old.startedAt,
    endedAt: update.endedAt || old.endedAt,
  };
}

const edgeKey = (e: GraphEdge) => JSON.stringify([e.from, e.to, e.kind]);

/** foldDelta folds one publication into the state. Additive by construction —
 *  a node named again updates the one already there, a repeated edge is the
 *  same edge — which is what makes a frame delivered twice harmless. */
export function foldDelta(s: GraphState, delta: GraphDelta): GraphState {
  // Copied on the first record worth keeping, so a delta that says nothing new
  // returns the state it was given and every memo downstream holds.
  let next: GraphState | null = null;
  const open = () =>
    (next ??= { nodes: s.nodes.slice(), edges: s.edges.slice(), at: new Map(s.at), seen: new Set(s.seen) });

  for (const incoming of delta.nodes ?? []) {
    if (!incoming.id) continue;
    const g = open();
    const at = g.at.get(incoming.id);
    if (at !== undefined) g.nodes[at] = merge(g.nodes[at], incoming);
    else {
      g.at.set(incoming.id, g.nodes.length);
      g.nodes.push(incoming);
    }
  }
  for (const edge of delta.edges ?? []) {
    if (!edge.from || !edge.to || !edge.kind || edge.from === edge.to) continue;
    const key = edgeKey(edge);
    if ((next ?? s).seen.has(key)) continue;
    const g = open();
    g.seen.add(key);
    g.edges.push(edge);
  }
  return next ?? s;
}

/** graphOf indexes the kernel's own fold. It is not a delta applied to an empty
 *  state: the snapshot replaces what a view holds rather than being folded into
 *  it, and none of it is a transition that just happened. */
export function graphOf(graph: ExecutionGraph): GraphState {
  const out: GraphState = { nodes: [], edges: [], at: new Map(), seen: new Set() };
  for (const node of graph.nodes ?? []) {
    if (!node.id || out.at.has(node.id)) continue;
    out.at.set(node.id, out.nodes.length);
    out.nodes.push(node);
  }
  for (const edge of graph.edges ?? []) {
    if (!edge.from || !edge.to || !edge.kind || edge.from === edge.to) continue;
    const key = edgeKey(edge);
    if (out.seen.has(key)) continue;
    out.seen.add(key);
    out.edges.push(edge);
  }
  return out;
}

// One fan-out, arranged for reading. A rank is a column: everything in it was
// free to run at the same moment, so concurrency is what the eye sees when a
// column is wide and ordering is what it sees when the graph is long.
export interface Lane {
  group: GraphNode;
  ranks: GraphNode[][];
  edges: GraphEdge[];
  // Adopted node id → the finished work whose answer stood in for running it.
  reuse: Record<string, string>;
  // Node id → the dependencies that have not answered yet. This is the whole
  // reason to draw the thing: a list can say a node has not started, only the
  // graph can say what it is still waiting for.
  blocked: Record<string, string[]>;
  ran: number;
  reused: number;
  // Members holding no slot yet. This is the fan-out the concurrency ceiling is
  // throttling right now, which no edge on the picture can say.
  queued: number;
}

const ANSWERED = new Set(["completed", "adopted"]);

/** lanesOf groups the folded graph into one lane per fan-out, ranking each
 *  lane's workers by how far down its dependency chain they sit. */
export function lanesOf(s: GraphState): Lane[] {
  const byID = new Map(s.nodes.map((n) => [n.id, n]));
  const groups = s.nodes.filter((n) => n.kind === "group");
  return groups.map((group) => lane(group, s, byID));
}

function lane(group: GraphNode, s: GraphState, byID: Map<string, GraphNode>): Lane {
  const members = s.nodes.filter((n) => n.parentId === group.id);
  const ids = new Set(members.map((n) => n.id));
  const inside = s.edges.filter((e) => ids.has(e.to) && (e.kind === "depends" || e.kind === "context"));

  const reuse: Record<string, string> = {};
  for (const e of s.edges) {
    if (e.kind === "adopt" && ids.has(e.to)) reuse[e.to] = byID.get(e.from)?.ref || e.from;
  }

  const deps = new Map<string, string[]>();
  for (const e of inside) {
    if (e.kind !== "depends") continue;
    deps.set(e.to, [...(deps.get(e.to) ?? []), e.from]);
  }

  const blocked: Record<string, string[]> = {};
  for (const node of members) {
    const unmet = (deps.get(node.id) ?? []).filter((id) => !ANSWERED.has(byID.get(id)?.state ?? ""));
    if (unmet.length > 0 && !ANSWERED.has(node.state ?? "")) blocked[node.id] = unmet;
  }

  const rank = ranker(deps, ids);
  const ranks: GraphNode[][] = [];
  for (const node of members) (ranks[rank(node.id)] ??= []).push(node);
  for (let i = 0; i < ranks.length; i++) ranks[i] ??= [];

  return {
    group,
    ranks,
    edges: inside,
    reuse,
    blocked,
    ran: members.filter((n) => n.state !== "adopted").length,
    reused: members.filter((n) => n.state === "adopted").length,
    queued: members.filter((n) => n.state === "queued").length,
  };
}

// Depth along the dependency chain, memoised. `busy` tracks the path being
// walked, not everything visited: a node reached twice through a diamond has a
// rank, and treating the second visit as a cycle would report it as a root.
// The guard itself is not tidiness — the kernel rejects cycles at preflight,
// but a stream that arrives truncated must still lay out rather than hang.
function ranker(deps: Map<string, string[]>, ids: Set<string>): (id: string) => number {
  const done = new Map<string, number>();
  const busy = new Set<string>();
  const rank = (id: string): number => {
    const known = done.get(id);
    if (known !== undefined) return known;
    if (busy.has(id)) return 0;
    busy.add(id);
    const from = (deps.get(id) ?? []).filter((d) => ids.has(d));
    const at = from.length === 0 ? 0 : 1 + Math.max(...from.map(rank));
    busy.delete(id);
    done.set(id, at);
    return at;
  };
  return rank;
}

// One step of the run, read along time rather than along dependency. lanesOf
// answers "what waited on what"; this answers "what happened, then what" — the
// same fact the kernel published, read on the other axis. A fan-out's members
// hang under the step that spawned them, because that is what the step turned
// out to be, not five steps standing beside it.
export interface Step {
  node: GraphNode;
  members: GraphNode[];
  // Upstreams that have not answered yet, named. Empty when nothing blocks it.
  blocked: string[];
}

/** stepsOf lists the run's top-level nodes in the order they started. */
export function stepsOf(s: GraphState): Step[] {
  const byID = new Map(s.nodes.map((n) => [n.id, n]));
  const deps = new Map<string, string[]>();
  for (const e of s.edges) {
    if (e.kind !== "depends") continue;
    deps.set(e.to, [...(deps.get(e.to) ?? []), e.from]);
  }
  const kids = new Map<string, GraphNode[]>();
  for (const n of s.nodes) {
    if (n.parentId) kids.set(n.parentId, [...(kids.get(n.parentId) ?? []), n]);
  }
  // A node whose parent is not on the page is a top-level step here: the
  // alternative is dropping it, and a truncated stream would silently shorten
  // the run rather than showing what it does hold.
  const top = s.nodes.filter((n) => !n.parentId || !byID.has(n.parentId));
  return ordered(top).map((node) => ({
    node,
    members: ordered(kids.get(node.id) ?? []),
    blocked: (deps.get(node.id) ?? [])
      .filter((id) => !ANSWERED.has(byID.get(id)?.state ?? ""))
      .map((id) => byID.get(id)?.label || id),
  }));
}

// Publication order is the tiebreak, not a fallback to zero: a node the kernel
// has not started carries no startedAt, and treating that as time zero would
// sort the run's future above its past.
function ordered(nodes: GraphNode[]): GraphNode[] {
  return nodes
    .map((node, i) => ({ node, i }))
    .sort((a, b) => {
      const x = a.node.startedAt ?? 0;
      const y = b.node.startedAt ?? 0;
      if (x && y) return x === y ? a.i - b.i : x - y;
      if (x !== y) return x ? -1 : 1;
      return a.i - b.i;
    })
    .map((p) => p.node);
}
