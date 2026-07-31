import type { Point } from './geometry.js';
import type { Edge, Node } from '../../../types.js';
type PointLite = Point;
type MaterializedEdge = Edge & {
    points?: PointLite[];
};
type MaterializedNode = Node & {
    direction?: string;
};
export declare function separateSharedRenderedTerminalLanes(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function collapseRedundantRectangularDoglegs(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function liftObstacleHuggingSameSideRails(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function liftTopLaneTitleBandsAboveRails(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function shiftLeftLaneTitleBandsLeftOfRails(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function swapDestinationTerminalTailsToReduceCrossings(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function reassignCrossingExternalRailChannels(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function shortcutRedundantOrthogonalJogs(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export declare function resolveRenderedOrthogonalCrossings(edges: MaterializedEdge[], nodeByIdMap: Map<string, MaterializedNode>): void;
export {};
