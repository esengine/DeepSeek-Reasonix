import type { Graph, NodeId } from './helpers.js';
/**
 * Greedy local search that tries to lift nodes to reduce crossings while
 * preserving acyclicity (rank(v) e= rank(u)+1 for every edge u-\>v).
 */
export declare function optimizeRanksByCrossings(g: Graph, initialRank: Record<NodeId, number>): Record<NodeId, number>;
