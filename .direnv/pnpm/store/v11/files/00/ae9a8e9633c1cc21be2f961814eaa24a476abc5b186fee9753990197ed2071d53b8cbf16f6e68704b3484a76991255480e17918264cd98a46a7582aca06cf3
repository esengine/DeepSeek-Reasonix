import type { Graph, Layering, OrderedLayers, Coordinates, Edge } from './helpers.js';
export interface LayoutOptions {
    compactSingleInput?: boolean;
    ignoreCrossLaneEdges?: boolean;
    optimizeRanksByCrossings?: boolean;
    automaticLaneOrdering?: boolean;
    layerGap?: number;
    nodeGap?: number;
    direction?: 'TB' | 'LR' | 'BT' | 'RL';
}
export interface LayoutResult {
    acyclic: Graph;
    reversed: Edge[];
    layering: Layering;
    ordered: OrderedLayers;
    coordinates: Coordinates;
}
export declare function sugiyamaLayout(g: Graph, opts?: LayoutOptions): LayoutResult;
