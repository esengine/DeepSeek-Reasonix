import type { Graph, NodeId } from './helpers.js';
import type { DrivingTree } from './driving-tree.js';
/**
 * Computes crossing counts for each parent/child pair in the driving tree.
 *
 * Uses a binary-lifting LCA over the driving tree and counts, for every edge,
 * how many layers it spans below the LCA. These per-layer counts are then
 * accumulated to obtain, for each parent -\> child edge in the tree, an
 * approximate "subtree crossing" score used when ordering children.
 */
export declare function computeSubtreeCrossCounts(g: Graph, rankOf: Record<NodeId, number>, tree: DrivingTree): Map<NodeId, Map<NodeId, number>>;
