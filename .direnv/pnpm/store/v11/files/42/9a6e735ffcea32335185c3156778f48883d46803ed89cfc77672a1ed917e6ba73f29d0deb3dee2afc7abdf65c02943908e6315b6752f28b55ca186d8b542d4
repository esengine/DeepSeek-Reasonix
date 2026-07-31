/**
 * Iter 16 — collapse a short terminal stub at an edge's destination by
 * retargeting the destination face and dropping the corner. See the call-
 * site comment in `applySwimlaneDirectionTransform` for the user report and
 * paper backing (Siebenhaller `21f7ca55`; precedent in the retired
 * stale-port-offset straightener, Hegemann-Wolff `b65b3d45`).
 *
 * Shape handled:
 *   ... → prev → penult → end
 *                ~~~~~~~~~~~~~
 *                penult perpendicular to last; `|end - penult| < MIN_STUB`.
 *
 * Rewrite:
 *   ... → prev' → end'
 *   where prev' keeps prev's "far" coordinate and adopts the destination's
 *   face-center on the other axis, and end' is the destination face-center
 *   on the approach axis (i.e. bottom/top when penult is vertical;
 *   left/right when penult is horizontal — face chosen opposite to the
 *   approach direction).
 *
 * Safety:
 *   - Reject when the new prev'→end' segment would enter any real-node
 *     rect (excluding the dst itself), any label rect, or cross an
 *     existing segment of a different edge (excluding its own segments).
 *   - Reject when the new prev' lies inside the src node's rect.
 *   - Only applied to the dst end; applying symmetrically on src
 *     would need identical safety accounting and is out of scope here.
 */
export declare function collapseShortTerminalStub(edges: any[], nodeByIdMap: Map<string, any>): void;
