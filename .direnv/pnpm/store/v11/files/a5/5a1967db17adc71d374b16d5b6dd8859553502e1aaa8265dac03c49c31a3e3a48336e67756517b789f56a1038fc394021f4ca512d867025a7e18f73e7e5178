/**
 * Line jumps ("hops") for edge crossings.
 *
 * Detects true segment crossings between edge polylines and rewrites the SVG
 * path of the later edge so the crossing renders as either a small arc
 * (`jumpStyle: 'arc'`) or a visible break (`jumpStyle: 'gap'`).
 *
 * The pure functions (`findEdgeIntersections`, `processEdgesWithJumps`) are
 * DOM-free. The DOM-side `applyLineJumpsToSvg` helper reads geometry from
 * layout data and leaves curved (non-`M`/`L`) rendered paths untouched.
 */
import type { D3Selection } from '../../types.js';
export interface Point {
    x: number;
    y: number;
}
export interface EdgeGeom {
    id: string;
    points: Point[];
    /**
     * Optional curve hint matching `edge.curve` from the rendering layer.
     * When set, line jumps are only applied for orthogonal-friendly curves
     * (`'linear'`, `'rounded'`, `'step'`, `'stepBefore'`, `'stepAfter'`, or
     * undefined). Other curves (basis, monotoneX, …) are skipped to avoid
     * corrupting smoothed geometry.
     */
    curve?: string;
    /** Arrow type at the start (first point) — used to apply marker offset so
     * the rewritten path's endpoint matches the original rendered geometry and
     * the arrow marker orients correctly. */
    arrowTypeStart?: string;
    /** Arrow type at the end (last point). */
    arrowTypeEnd?: string;
}
export interface LineJumpConfig {
    enabled: boolean;
    jumpRadius: number;
    jumpStyle: 'arc' | 'gap';
}
export interface Crossing {
    jumpEdgeId: string;
    otherEdgeId: string;
    /** Index of the segment within the jumping edge's polyline. */
    segIndex: number;
    /** Position of the crossing along the jumping edge's segment, 0..1. */
    t: number;
    point: Point;
}
export declare function findEdgeIntersections(edges: EdgeGeom[]): Crossing[];
export declare function processEdgesWithJumps(edges: EdgeGeom[], config: LineJumpConfig): Map<string, string>;
/**
 * Returns true iff the SVG path `d` is a straight-line path — only `M`/`L`/`m`/`l`
 * move/line commands plus their numeric coordinates (digits, sign, decimal point,
 * scientific-notation `e`, and `,`/space separators). Curved paths are skipped by
 * the caller.
 */
export declare function isStraightPath(d: string): boolean;
/**
 * Returns true iff the named curve produces orthogonal-friendly segments that
 * can be safely re-emitted with line jumps. Includes `'rounded'` even though
 * its rendered `d` contains `Q` corner-rounding commands — when an edge with
 * a jump is rewritten the corner rounding is dropped in exchange for visible
 * arc hops at crossings, which is the desired trade-off.
 */
export declare function curveSupportsLineHops(curve: string | undefined): boolean;
/**
 * Patches the rendered SVG paths in `edgePathsGroup` for any edges that
 * cross. The true geometry is read from each path's `data-points` attribute
 * (written by edges.js at render time) so the rewrite's endpoints match
 * exactly what was originally rendered. Edges whose curve is a true
 * smoothing curve (`basis`, `monotoneX`, …) are skipped.
 */
export declare function applyLineJumpsToSvg(edgePathsGroup: D3Selection<SVGGElement>, edges: EdgeGeom[], config: LineJumpConfig): void;
