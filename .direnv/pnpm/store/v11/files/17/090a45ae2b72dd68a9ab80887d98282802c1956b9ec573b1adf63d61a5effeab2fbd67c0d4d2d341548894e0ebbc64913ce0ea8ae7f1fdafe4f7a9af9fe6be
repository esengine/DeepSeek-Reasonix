import type { Selection } from 'd3';
import type { LayoutData } from '../../types.js';
type D3Selection<T extends SVGElement = SVGElement> = Selection<T, unknown, Element | null, unknown>;
/**
 * Whether DDLT size capture is enabled. Kept here so callers can avoid importing
 * this module at all in production — the createGraph guard reads the raw
 * `globalThis.mermaidCaptureSizes` flag directly and only dynamically imports
 * this module when it is truthy.
 */
export declare function shouldCaptureSizes(): boolean;
/**
 * Record the measured sizes of every leaf + edge-label node from an
 * already-laid-out {@link LayoutData} into `window.mermaidCapturedSizes`.
 *
 * Reads `node.width`/`node.height` set by createGraphWithElements during
 * measurement, so the capture lives entirely outside the production render path.
 *
 * @param element - The container the diagram was rendered into.
 * @param data4Layout - Layout data whose nodes have been measured.
 */
export declare function captureNodeSizes(element: D3Selection, data4Layout: LayoutData): void;
export {};
