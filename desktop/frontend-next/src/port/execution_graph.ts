// The run's execution graph, published as facts rather than drawn from id
// prefixes. Mirrors internal/contract/agentgraph: a node named again updates the one
// already there, and an edge kind is what tells "waited for this" apart from
// "started from this answer".
export type GraphNodeKind = "group" | "worker" | "external";

export type GraphNodeState =
  | "pending"
  // Ready and waiting for a session concurrency slot. Apart from "pending" on
  // purpose: nothing upstream is missing, only the ceiling stands in the way.
  | "queued"
  | "running"
  | "completed"
  | "adopted"
  | "failed"
  | "cancelled"
  | "skipped";

export type GraphEdgeKind = "spawn" | "depends" | "context" | "adopt";

// Absent means the question does not apply: a group is not a run, and an
// adopted node never got a grant. A bool could not say that apart from "writes".
export type GraphGrant = "read" | "write";

// Why a queued node is not running. "queued" is one word for constraints with
// different answers: the first two are session ceilings, and a claim is a write
// path someone else holds, which no ceiling releases.
export type GraphWait = "slots" | "writers" | "claim";

export interface GraphNode {
  id: string;
  parentId?: string;
  kind?: GraphNodeKind;
  state?: GraphNodeState;
  label?: string;
  profile?: string;
  model?: string;
  effort?: string;
  grant?: GraphGrant;
  wait?: GraphWait;
  ref?: string;
  queuedAt?: number;
  startedAt?: number;
  endedAt?: number;
  err?: string;
}

export interface GraphEdge {
  from: string;
  to: string;
  kind: GraphEdgeKind;
}

export interface GraphDelta {
  nodes?: GraphNode[];
  edges?: GraphEdge[];
}

// The folded graph, as the kernel's durable facts justify it. Same two arrays a
// delta carries and a different thing entirely: this one is a state, not a
// publication, and it is replaced rather than folded.
export interface ExecutionGraph {
  nodes?: GraphNode[];
  edges?: GraphEdge[];
}

// One execution the host found open with nobody running it. kind separates work
// that may be half-done from work that never began; neither resumes.
export interface ExecutionInterruption {
  execution: string;
  kind: "interrupted-before-start" | "interrupted-during-execution";
}

// The whole execution read model, and the only authority for the two lists
// beside the graph: no delta carries them, so nothing can patch them in later.
export interface ExecutionGraphSnapshot {
  graph: ExecutionGraph;
  interruptions?: ExecutionInterruption[];
  // Executions whose worker layer was never recorded. Their model reads empty
  // because nothing was observed, which an inherited one does not.
  identityUnknown?: string[];
}

// The envelope the snapshot arrives in: which conversation it describes, and
// the frame it is at least as new as.
export interface ExecutionGraphView {
  schemaVersion: number;
  sessionId: string;
  watermark: number;
}

export type ExecutionGraphRead = ExecutionGraphView & ExecutionGraphSnapshot;
