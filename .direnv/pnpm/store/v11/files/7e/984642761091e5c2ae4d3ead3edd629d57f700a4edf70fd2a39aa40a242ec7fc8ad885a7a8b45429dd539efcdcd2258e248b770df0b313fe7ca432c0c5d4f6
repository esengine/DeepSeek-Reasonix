import type { LayoutData } from '../../types.js';
import type { SizesFixture } from './types.js';
export declare function loadSizesFixture(path: string): SizesFixture;
export declare function loadFreshSizesFixture(sizesPath: string, mmdPath: string, fixtureId: string): SizesFixture;
export interface SyntheticSizesOptions {
    /** Minimum width applied to every node. */
    minWidth?: number;
    /** Height applied to every node. */
    height?: number;
    /** Approximate per-character width used to grow nodes with long labels. */
    charWidth?: number;
    /** Extra horizontal padding around the label. */
    padding?: number;
}
/**
 * DOM-free stand-in for `createGraphWithElements` content sizing.
 *
 * Use this in DDLT specs whose inputs are synthetic (inline `flowchart` strings
 * or hand-built `LayoutData`) and therefore have no captured `.sizes.json`.
 * Sizes are deterministic functions of the label length so the layout
 * algorithm sees realistic-but-stable rectangles without touching JSDOM.
 *
 * For specs backed by a real `.mmd` fixture, prefer
 * {@link applyFixtureContentSizesStrict} so the test reflects production sizing.
 */
export declare function applySyntheticContentSizes(layout: LayoutData, options?: SyntheticSizesOptions): void;
/** DOM-free stand-in for sizing `isEdgeLabel: true` dummy label nodes. */
export declare function applySyntheticLabelSizes(layout: LayoutData, options?: SyntheticSizesOptions): void;
/** Apply captured bbox sizes to non-group content nodes (strict: every non-group must have a fixture row). */
export declare function applyFixtureContentSizesStrict(layout: LayoutData, fixture: SizesFixture): void;
/** Apply sizes to nodes with `isEdgeLabel` (strict). */
export declare function applyFixtureLabelSizesStrict(layout: LayoutData, fixture: SizesFixture): void;
