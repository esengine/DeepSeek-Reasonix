import type { Graph, NodeId } from './helpers.js';
export interface DrivingTreeBlock {
    id: number;
    nodes: NodeId[];
    edges: [NodeId, NodeId][];
}
export interface DrivingTree {
    parent: Map<NodeId, NodeId | null>;
    children: Map<NodeId, NodeId[]>;
    roots: NodeId[];
    componentOf: Map<NodeId, number>;
    blocks: DrivingTreeBlock[];
    nodeBlocks: Map<NodeId, number[]>;
    adjacency: Map<NodeId, NodeId[]>;
    preorder: NodeId[];
    postorder: NodeId[];
    topologicalOrder: NodeId[];
}
export interface DrivingTreeBuildOptions {
    rankHint?: Record<NodeId, number>;
    laneOf?: (id: NodeId) => string | null;
}
export declare function buildDrivingTree(graph: Graph, opts?: DrivingTreeBuildOptions): DrivingTree;
