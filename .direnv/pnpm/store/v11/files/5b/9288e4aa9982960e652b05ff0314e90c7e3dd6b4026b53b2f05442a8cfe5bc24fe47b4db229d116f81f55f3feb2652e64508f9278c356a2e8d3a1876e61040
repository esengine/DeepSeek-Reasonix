import type { LayoutData } from '../../types.js';
export type LayoutIssueType = 'node-overlap' | 'edge-missing-points' | 'edge-non-orthogonal' | 'edge-intersects-node' | 'edge-intersects-obstacle' | 'edge-intersects-group-title' | 'edge-port-direction-mismatch' | 'edge-same-port-departure' | 'edge-shared-attachment-point' | 'edge-shared-projected-port' | 'edge-bend-near-endpoint' | 'edge-corner-connection' | 'edge-shared-subpath' | 'edge-parallel-segment-too-close' | 'edge-border-hugging' | 'node-border-hugging' | 'edge-label-off-edge' | 'edge-endpoint-inside-node' | 'edge-label-overlaps-foreign-edge' | 'edge-label-overlaps-own-arrowhead' | 'edge-label-overlaps-group-border';
export interface Issue {
    type: LayoutIssueType;
    message: string;
    nodeIds?: string[];
    edgeId?: string;
    details?: Record<string, unknown>;
}
export interface ValidateLayoutResult {
    ok: boolean;
    issues: Issue[];
    /**
     * DDLT headline score in [0, 1000]. **Zero** when `!ok`. When `ok`, starts
     * at 1000 and is reduced by `totalBendPenalty` (per-edge by polyline point
     * count, exponential past 6) plus `crossingPenalty`. Clamped to [0, 1000].
     */
    score: number;
    breakdown: {
        /** Number of leaf nodes (excluding groups). */
        nodeCount: number;
        /** Number of valid edges (with at least 2 points). */
        edgeCount: number;
        /** Crossing events counted globally. */
        crossings: number;
        /** Total points across all edges (for sanity / debugging). */
        totalPoints: number;
        /** Sum of per-edge bend penalties. */
        totalBendPenalty: number;
        /** Crossings * CROSSING_PENALTY. */
        crossingPenalty: number;
        /** Per-edge breakdown sorted DESC by `bendPenalty` (worst offenders first). */
        edges: {
            id: string;
            points: number;
            bendPenalty: number;
        }[];
        /** Histogram of polyline point counts: keys '2','3','4','5','6','7+'. */
        pointsHistogram: Record<'2' | '3' | '4' | '5' | '6' | '7+', number>;
    };
}
/**
 * Step 0: Validate a computed orthogonal layout for basic geometric invariants.
 *
 * Checks:
 * - No box overlaps (excluding ancestor containment for groups).
 * - Edge polylines are orthogonal.
 * - Edge segments do not intersect node/obstacle interiors.
 * - Segment leaving/entering a *boundary* port goes outward from that side.
 * - Edges don't depart from same port with same direction.
 * - Edges don't connect at node corners.
 * - Edges don't share subpaths.
 * - Edges don't hug node borders.
 *
 * Also computes scoring based on bends and crossings.
 */
export declare function validateLayout(layout: LayoutData): ValidateLayoutResult;
