import type { Graph, NodeId } from './helpers.js';
/**
 * Options controlling how layers are assigned in the Sugiyama pipeline.
 */
export interface LayeringOptions {
    /** If true, a node with exactly one incoming edge inherits its predecessor's layer. */
    compactSingleInput?: boolean;
    /** If true, ignore edges from other lanes when calculating layer positions. */
    ignoreCrossLaneEdges?: boolean;
    /** If true, try to lift nodes to reduce crossings between layers. */
    optimizeRanksByCrossings?: boolean;
    /** Diagram direction, used by lane-aware rank heuristics with direction-specific failure modes. */
    direction?: 'TB' | 'LR' | 'BT' | 'RL';
}
/**
 * Computes the "top lane" (outermost group container) for each node.
 *
 * Returns a map from node id -\> lane id (top-level group) or null if the node
 * does not belong to any lane.
 */
export declare function buildTopLaneMap(g: Graph): Map<NodeId, string | null>;
export declare function createTopLaneResolver(g: Graph): (id: NodeId) => string | null;
export declare function buildTopLaneOrder(g: Graph): string[];
export declare function resolveTopLaneOrder(g: Graph, preferredOrder?: string[]): string[];
