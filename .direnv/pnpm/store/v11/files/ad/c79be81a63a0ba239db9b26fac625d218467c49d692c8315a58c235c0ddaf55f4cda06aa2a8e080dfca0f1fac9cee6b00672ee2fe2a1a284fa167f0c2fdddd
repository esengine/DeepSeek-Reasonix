import type { LayoutData } from '../../types.js';
export interface ParseToLayoutDataOptions {
    /** When true, stamp `type`, `markers`, spacing like `flowRenderer-v3-unified.ts`. */
    stampFlowchartRendererFields?: boolean;
}
/**
 * Parse a `.mmd` file through the real diagram pipeline (preprocess → Diagram.fromText → getData).
 * Mirrors production flowchart hand-off; callers should set `direction` / `layoutAlgorithm` as needed.
 */
export declare function parseMmdFileToLayoutData(mmdPath: string, options?: ParseToLayoutDataOptions): Promise<LayoutData>;
