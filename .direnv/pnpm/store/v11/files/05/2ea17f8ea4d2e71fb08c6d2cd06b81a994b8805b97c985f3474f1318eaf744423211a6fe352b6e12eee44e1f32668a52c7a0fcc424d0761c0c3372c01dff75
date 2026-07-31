/**
 * Layout quality scoring function (Level 2 validation).
 *
 * Computes soft quality metrics for a computed layout and returns numeric scores.
 * Independent of validateLayout (Level 1) — they share geometry helpers but
 * neither calls the other.
 */
import type { LayoutData } from '../../types.js';
export interface LayoutScores {
    edgeLengthRatio: number;
    aspectRatio: number;
    avgBendsPerEdge: number;
    totalBends: number;
    crossings: number;
    rankFaithfulness: number;
    neighborhoodPreservation: number;
    /**
     * NOT YET IMPLEMENTED — always `NaN`. Symmetry detection is unfinished, so
     * this is a placeholder. A NaN value fails any threshold a caller sets on it
     * (see `evaluateThresholds`), and no current caller scores against it, so the
     * overall layout assessment is not symmetry-aware. TODO: implement.
     */
    symmetryScore: number;
    boundingBoxArea: number;
    straightEdgeRatio: number;
    /**
     * Count of edges whose first or last RENDERED segment is non-axis-aligned.
     * Mirrors `rendering-elements/edges.js:426-462` — the renderer recomputes
     * endpoint boundary intersections with `intersect.rect(node, inner_point)`,
     * which draws a ray from node center through inner_point; if inner_point
     * is offset from the center in both axes, the boundary snap lands at a
     * different perpendicular offset, producing a diagonal final segment.
     * An edge counts as 1 if its first OR last rendered segment is diagonal;
     * an edge with both first and last diagonal also counts as 1 (per-edge count).
     */
    renderedDiagonalEndpoints: number;
}
export interface ScoreLayoutResult {
    scores: LayoutScores;
    thresholdResults: Record<string, {
        value: number;
        threshold: string;
        pass: boolean;
    }> | null;
}
export declare function scoreLayout(layout: LayoutData, thresholds?: Partial<Record<keyof LayoutScores, {
    min?: number;
    max?: number;
}>>): ScoreLayoutResult;
