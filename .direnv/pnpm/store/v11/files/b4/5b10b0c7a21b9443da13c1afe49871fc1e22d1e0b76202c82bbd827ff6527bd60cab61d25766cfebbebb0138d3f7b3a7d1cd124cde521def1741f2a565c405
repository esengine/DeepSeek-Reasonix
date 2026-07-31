import type { Edge, Node } from '../../../types.js';
/**
 * Iter 12 — co-route sibling straight-line rescue.
 *
 * Fires only on the narrow "4-point U-detour around a collinear blocker
 * where the obvious straight line is geometrically clear" shape. For each
 * eligible edge, shifts the source and destination attach points by
 * MIN_PORT_SPACING/2 along the shared face and replaces the polyline
 * with a 2-point straight line. The shift direction is chosen by trying
 * both +delta and -delta and picking whichever doesn't introduce a new
 * edge crossing or leave the node's face span.
 *
 * Paper backing: Hegemann & Wolff "On the smoothing of orthogonal
 * connector layouts" (NotebookLM src b65b3d45) §4.2 / Fig. 11 —
 * joint-feasibility via port distribution rather than face exclusion.
 * Mermaid-specific narrowing: we only rescue the exact 4-point shape to
 * minimize blast radius.
 */
export declare function straightenCollinearSiblingDetours(edges: Edge[], nodes: Node[]): void;
