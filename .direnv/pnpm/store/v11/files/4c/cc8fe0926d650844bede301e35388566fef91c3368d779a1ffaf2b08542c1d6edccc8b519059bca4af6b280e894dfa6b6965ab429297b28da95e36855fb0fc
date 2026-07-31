export interface Point {
    x: number;
    y: number;
}
export type RectSide = 'top' | 'bottom' | 'left' | 'right';
export interface RectBounds {
    left: number;
    right: number;
    top: number;
    bottom: number;
}
export interface RectEntry {
    id: string;
    rect: RectBounds;
}
export interface NodeBoundsInfo extends RectEntry {
    cx: number;
    cy: number;
}
export interface NodePairGeometry {
    srcId: string;
    dstId: string;
    srcInfo: NodeBoundsInfo;
    dstInfo: NodeBoundsInfo;
    collinearX: boolean;
    collinearY: boolean;
}
export interface LayoutNodeRect extends RectBounds {
    nodeId: string;
}
export interface ThreeSegmentRoute {
    kind: 'HVH' | 'VHV';
    p0: Point;
    p1: Point;
    p2: Point;
    p3: Point;
}
export interface OrthogonalSegment {
    index: number;
    a: Point;
    b: Point;
    horizontal: boolean;
    vertical: boolean;
}
interface EdgeSegmentInput {
    points?: Point[];
    isLayoutOnly?: boolean;
}
interface NodeBoundsInput {
    id?: string;
    x?: number;
    y?: number;
    width?: number;
    height?: number;
    isGroup?: boolean;
    isEdgeLabel?: boolean;
}
interface RectNodeInput {
    x?: number;
    y?: number;
    width?: number;
    height?: number;
}
interface EdgeEndpointInput {
    start?: string;
    end?: string;
}
export declare function samePoint(a: Point, b: Point, epsilon?: number): boolean;
export declare function sameX(a: Point, b: Point, epsilon?: number): boolean;
export declare function sameY(a: Point, b: Point, epsilon?: number): boolean;
export declare function isHorizontalSegment(a: Point, b: Point, epsilon?: number): boolean;
export declare function isVerticalSegment(a: Point, b: Point, epsilon?: number): boolean;
export declare function overlapLength(a1: number, a2: number, b1: number, b2: number): number;
export declare function sameAxisSegmentOverlapLength(a: OrthogonalSegment, b: OrthogonalSegment, epsilon?: number): number;
export declare function orthogonalSegmentsForPoints(points: Point[], epsilon?: number): OrthogonalSegment[];
export declare function countOrthogonalBends(points: Point[], epsilon?: number): number;
export declare function dedupeConsecutivePoints(points: Point[], epsilon?: number): Point[];
export declare function classifyThreeSegmentRoute(points: Point[] | undefined, epsilon?: number): ThreeSegmentRoute | undefined;
export declare function segmentBoundsOverlapRect(a: Point, b: Point, rect: RectBounds, buffer?: number): boolean;
export declare function pointInsideRect(point: Point, rect: RectBounds, buffer?: number): boolean;
export declare function rectContainsRect(outer: RectBounds, inner: RectBounds): boolean;
export declare function rectsOverlap(a: RectBounds, b: RectBounds): boolean;
export declare function inflateRect(rect: RectBounds, margin: number): RectBounds;
export declare function rectFromCenterSize(cx: number, cy: number, width: number, height: number): RectBounds;
export declare function rectOfNodeBounds(node: RectNodeInput): RectBounds | undefined;
export declare function portForRectSide(node: {
    cx: number;
    cy: number;
    rect: RectBounds;
}, side: RectSide): Point;
export declare function buildOrthogonalPortPath(src: Point, srcSide: RectSide, dst: Point, dstSide: RectSide, anchor: number, epsilon?: number): Point[] | undefined;
export declare function buildSameSideTrackPath(src: Point, side: RectSide, dst: Point, track: number): Point[];
export declare function collectRealNodeBounds(nodes: Iterable<NodeBoundsInput>): {
    nodeInfoById: Map<string, NodeBoundsInfo>;
    realNodeRects: RectEntry[];
};
export declare function collectNodeRectEntries(nodes: Iterable<NodeBoundsInput>): {
    realNodeRects: RectEntry[];
    labelNodeRects: RectEntry[];
};
export declare function collectLayoutNodeRects(nodes: Iterable<NodeBoundsInput>, { includeEdgeLabels }?: {
    includeEdgeLabels?: boolean;
}): LayoutNodeRect[];
export declare function getNodePairGeometry(edge: EdgeEndpointInput, nodeInfoById: Map<string, NodeBoundsInfo>, epsilon?: number): NodePairGeometry | undefined;
export declare function segmentHitsAnyRect(a: Point, b: Point, rects: RectEntry[], excludeIds?: string[], shrink?: number): boolean;
export declare function orthogonalSegmentsCross(a1: Point, b1: Point, a2: Point, b2: Point, epsilon?: number, endpointTolerance?: number): boolean;
export declare function sameAxisSegmentsOverlap(a1: Point, b1: Point, a2: Point, b2: Point, epsilon?: number): boolean;
export declare function segmentConflictsWithAnyEdge(a: Point, b: Point, edges: Iterable<EdgeSegmentInput>, excludeEdge?: EdgeSegmentInput, { epsilon, skipDegenerateOther, }?: {
    epsilon?: number;
    skipDegenerateOther?: boolean;
}): boolean;
export declare function orthogonalSegmentsStrictlyCross(a1: Point, b1: Point, a2: Point, b2: Point, epsilon?: number): boolean;
export declare function orthogonalizePolyline(pts: Point[]): Point[];
export declare function simplifyPolyline(pts: Point[]): Point[];
export {};
