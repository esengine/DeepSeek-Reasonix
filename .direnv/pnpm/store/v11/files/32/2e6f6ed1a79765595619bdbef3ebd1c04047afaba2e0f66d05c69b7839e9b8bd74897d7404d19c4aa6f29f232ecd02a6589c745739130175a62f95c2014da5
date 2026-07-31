import type { LayoutData } from '../../../types.js';
export interface ValidationIssue {
    type: 'edge-node-overlap' | 'edge-edge-crossing';
    edgeId: string;
    /** Second edge ID (for crossings) or node ID (for overlaps) */
    targetId: string;
    detail: string;
}
/**
 * Final validation pass: scans the completed layout for remaining quality
 * issues. Does not attempt fixes, just logs warnings so developers can
 * identify problems during debugging.
 *
 * Checks:
 * 1. Edge segments that still pass through non-endpoint nodes
 * 2. Edge segments that cross other edge segments
 */
export declare function validateSwimlanesLayout(layout: LayoutData): ValidationIssue[];
