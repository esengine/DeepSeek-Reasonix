import type { LayoutData } from './types.js';
/**
 * Shallow-safe clone for a DOM measurement pass: strips function fields that break `structuredClone`.
 * Used so `createGraphWithElements` can mutate a throwaway graph while results are copied back to canonical `LayoutData`.
 */
export declare function cloneLayoutDataForDomMeasure(layout: LayoutData): LayoutData;
/** Copy the measured `nodes`/`edges` back onto `canonical`, leaving its `config` untouched. */
export declare function copyMeasuredGraphOntoCanonical(canonical: LayoutData, measured: LayoutData): void;
