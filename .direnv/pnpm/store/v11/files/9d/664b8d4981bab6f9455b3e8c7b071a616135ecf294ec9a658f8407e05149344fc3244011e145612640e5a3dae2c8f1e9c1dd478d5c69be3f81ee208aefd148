/**
 * Edge Label Nodes Transformation (label-as-waypoint variant)
 *
 * For each labelled edge, this transform creates an `edge-label-*` node that
 * participates in the Sugiyama layout (so the label text gets a deterministic
 * position in a lane). Unlike the older split-edge model, it leaves the
 * original labelled edge in place and stamps `labelNodeId` on it — the router
 * uses that stamp to thread the original edge's single polyline through the
 * label node's center.
 *
 * Two `isLayoutOnly` virtual edges (A→label, label→B) are appended to the
 * layout so that Sugiyama's layering and ordering honour the label's position
 * between source and target. They are never routed or rendered: the router and
 * renderer skip any edge flagged with `isLayoutOnly`.
 */
import type { LayoutData } from '../../types.js';
/**
 * Transforms edges with labels into label nodes + layout-only virtual edges.
 *
 * For each edge with a label:
 * 1. Creates a label node with the label text.
 * 2. Assigns the label node to the source or target lane (cross-lane edges
 *    prefer the target lane for tighter routing).
 * 3. Stamps `labelNodeId` on the original edge.
 * 4. Appends two `isLayoutOnly: true` virtual edges (A→label, label→B) so
 *    Sugiyama places the label between source and target. The router skips
 *    these; only the original edge is routed (threading through the label
 *    node's center).
 *
 * @param data - The layout data to transform
 * @returns The transformed layout data with label nodes and virtual edges
 */
export declare function createEdgeLabelNodes(data: LayoutData): LayoutData;
