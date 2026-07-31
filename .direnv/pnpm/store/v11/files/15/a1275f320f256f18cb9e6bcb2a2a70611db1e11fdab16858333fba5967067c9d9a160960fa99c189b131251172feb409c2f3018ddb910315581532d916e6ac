/**
 * Orthogonal edge router for the swimlanes layout.
 *
 * Each edge is routed as an axis-aligned (Manhattan) polyline between ports on
 * the node borders, with port distribution and nudging to reduce overlaps and
 * crossings. The approach follows the orthogonal-connector-routing literature —
 * notably Wybrow, Marriott & Stuckey, "Orthogonal Connector Routing" (the
 * libavoid family). "Raykov" in the comments and tests is the informal name this
 * implementation was developed under, not an external dependency.
 */
import type { LayoutData } from '../../../types.js';
export declare function routeEdgesOrthogonal(data: LayoutData, direction?: string): LayoutData;
