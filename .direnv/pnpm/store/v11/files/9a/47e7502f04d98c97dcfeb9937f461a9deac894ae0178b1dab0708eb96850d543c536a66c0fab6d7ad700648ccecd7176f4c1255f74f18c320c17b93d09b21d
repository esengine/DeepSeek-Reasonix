import type { NodeId } from './helpers.js';
/**
 * Utilities shared by the multitree-based layer ordering logic.
 */
export declare function annotateMinimumLayers(nodes: NodeId[], children: Map<NodeId, NodeId[]>, rankOf: Record<NodeId, number>): Map<NodeId, number>;
export declare function compareByRankThenId(rankOf: Record<NodeId, number>): (a: NodeId, b: NodeId) => number;
/**
 * Emits nodes into layers in tree traversal order, using the provided
 * orderChildren function to determine the order of children.
 */
export declare function emitNodesInTreeOrder(roots: NodeId[], allNodes: NodeId[], rankOf: Record<NodeId, number>, orderChildren: (node: NodeId) => NodeId[]): NodeId[][];
/**
 * Removes duplicate nodes from each layer while preserving order.
 */
export declare function deduplicateLayers(layers: NodeId[][]): NodeId[][];
