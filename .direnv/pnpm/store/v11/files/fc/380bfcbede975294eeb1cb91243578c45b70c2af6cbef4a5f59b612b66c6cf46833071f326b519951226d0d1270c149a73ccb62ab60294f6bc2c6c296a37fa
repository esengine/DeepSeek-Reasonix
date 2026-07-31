import type { Edge, Node } from '../../../types.js';
/**
 * Iter 17 — port-swap a 4-point H-V-H / V-H-V edge to a 3-point L-shape
 * when the current src port is "straight-through" (parallel to the
 * incoming edge) but a perpendicular src face permits a one-bend reach
 * to the existing dst port.
 *
 * Motivating case (user report 2026-04-16, 8-query-process-2.mmd):
 *   L_A2_E_0 currently:
 *     (A2.east=355.2, 0) → (gutter=402.3, 0)
 *       → (402.3, 213.4) → (E.west=844.1, 213.4)
 *   i.e. exits A2 on the east face (parallel to incoming A→A2), bends
 *   south, bends east — 2 interior bends. The south face of A2 points
 *   directly toward E's lane, so exiting south gives a 1-bend L-shape:
 *     (A2.south=cx±δ, 69.3) → (cx±δ, 213.4) → (E.west=844.1, 213.4)
 *   Saves one bend.
 *
 * Paper backing:
 *   - Tamassia's bend-minimization flow (1987, and
 *     Di Battista–Eades–Tamassia–Tollis §5): port/face assignment is a
 *     free variable and the optimum switches faces whenever it saves a
 *     bend.
 *   - Kandinsky port distribution (Fößmeier–Kaufmann 1995;
 *     Siebenhaller dissertation §2.3–§2.5): decision-diamond outgoing
 *     edges favor distinct perpendicular faces.
 *   - Siebenhaller §3.3 "Port Assignment" + §4.1 "Bend optimization":
 *     local port-swap accepted iff (a) bends strictly decrease, (b) no
 *     new crossings, (c) Kandinsky face-capacity preserved.
 *   - Hegemann–Wolff §4.2 joint-feasibility (paper src `b65b3d45`):
 *     the formal crossings + capacity guard set.
 *
 * Shape handled (src, dst NOT collinear):
 *   H-V-H:  p0 → p1 (horiz) → p2 (vert) → p3 (horiz);
 *           src face E/W (parallel to seg01); swap to N/S.
 *   V-H-V:  p0 → p1 (vert)  → p2 (horiz) → p3 (vert);
 *           src face N/S (parallel to seg01); swap to E/W.
 *
 * Rewrite (H-V-H):
 *   new_src_port = (src.cx + δ, dst-below ? src.bottom : src.top)
 *   new_polyline = [ new_src_port, (src.cx + δ, p3.y), p3 ]
 * (V-H-V symmetric across axes.)
 *
 * Safety (the six guards from the iter-17 plan):
 *   1. Strict bend-count decrease   — enforced by the 4-point → 3-point
 *                                     rewrite.
 *   2. No new edge-edge crossings    — orthogonalSegmentsCross vs every other
 *                                     non-self segment.
 *   3. No new edge-node collisions   — segment-vs-node guard (both new segs,
 *                                     excluding src for seg-1 and dst
 *                                     for seg-2).
 *   4. Kandinsky face capacity       — port-offset delta chosen from
 *                                     0, ±PORT_SHIFT, ±2·PORT_SHIFT;
 *                                     each candidate must lie strictly
 *                                     within the src face span; the
 *                                     collinear-axis overlap check
 *                                     (shared axis + overlapping range)
 *                                     rejects δ values that collide
 *                                     with an existing sibling port.
 *   5. No label-rect overlap on new  — re-done by anchorLabelsToPolyline
 *      segments                        which runs after this pass.
 *   6. Monotonic on fixture suite    — enforced externally by the DDLT
 *                                     contract (no spec's totalBends or
 *                                     crossings may increase).
 *
 * Distinct from `straightenCollinearSiblingDetours` (iter 12) which handles
 * the COLLINEAR case (4-point → 2-point straight). This pass handles
 * the non-collinear case (4-point → 3-point L). The two are disjoint
 * by the collinearX === collinearY guard: coRoute runs first and
 * converts collinear edges to 2-point straights which this pass then
 * skips by shape filter.
 *
 * Distinct from Eiglsperger bend-stretching (cited in iter 16
 * collapseShortTerminalStub): that pass requires the first and last
 * direction to be preserved; this pass explicitly CHANGES the first
 * direction — the whole point.
 */
export declare function portSwapToLShape(edges: Edge[], nodes: Node[]): void;
