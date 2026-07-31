import type { Graph } from './helpers.js';
export declare const AUTOMATIC_LANE_ORDERING_RESTARTS = 8;
export interface WeightedLaneEdge {
    a: string;
    b: string;
    weight: number;
}
export declare function laneArrangementCost(order: string[], weights: WeightedLaneEdge[]): number;
export declare function buildWeightedLaneEdges(g: Graph): WeightedLaneEdge[];
export declare function optimizeTopLaneOrder(g: Graph, opts?: {
    restarts?: number;
}): string[];
