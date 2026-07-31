import type { Graph, EdgeRef, NodeId } from './helpers.js';
export declare function normalizeGraph(g: Graph): Graph;
export declare function incoming(g: Graph, v: NodeId): EdgeRef[];
export declare function outgoing(g: Graph, v: NodeId): EdgeRef[];
export declare function buildSuccessorMap(g: Graph): Map<NodeId, NodeId[]>;
export declare function buildSortedSuccessorMap(g: Graph): Map<NodeId, NodeId[]>;
export declare function buildInDegreeMap(g: Graph): Map<NodeId, number>;
export declare function sortedZeroInDegreeNodes(indeg: Map<NodeId, number>): NodeId[];
export declare function buildPredecessorSuccessorMaps(g: Graph, includeEdge?: (edge: EdgeRef) => boolean): {
    preds: Map<NodeId, NodeId[]>;
    succs: Map<NodeId, NodeId[]>;
};
export declare function buildLayersFromRanks(g: Graph, order: NodeId[], rankOf: Record<NodeId, number>, opts?: {
    skipGroups?: boolean;
}): NodeId[][];
export declare function isAcyclic(g: Graph): boolean;
export declare function topoSortIfAcyclic(g: Graph): NodeId[] | null;
/**
 * Build an index map from node IDs to their positions in a layer.
 * This is used for efficient lookups during crossing minimization.
 */
export declare function buildLayerIndex(layer: NodeId[]): Map<NodeId, number>;
export declare function countInversions(values: number[]): number;
