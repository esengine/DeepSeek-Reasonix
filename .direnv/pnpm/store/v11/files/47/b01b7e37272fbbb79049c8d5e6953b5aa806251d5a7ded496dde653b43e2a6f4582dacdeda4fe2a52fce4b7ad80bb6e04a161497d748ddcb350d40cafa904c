import type { LayoutData } from '../../types.js';
import type { LayoutTestBackend, LayoutTestBackendId, OrthogonalTrace } from './types.js';
import type { DdltFixtureProfile, SizesFixture } from './types.js';
/**
 * DOMUS orthogonal routing + overlay finalize. **Not available on this branch.**
 * Throws on call so any accidental usage of the domus backend surfaces as a
 * loud error rather than a silent no-op.
 */
export declare function runDomusOrthogonalDdlt(_layout: LayoutData, _options?: {
    trace?: OrthogonalTrace;
}): Promise<void>;
/**
 * Swimlanes pipeline (mirrors `swimlanes/query-process.ddlt.spec.ts`).
 * Mutates `layout` to hold the finished `LayoutData` from the swimlanes subgraph.
 */
export declare function runSwimlanesDdlt(layout: LayoutData, sizes: SizesFixture): void;
/**
 * Parse `.mmd`, apply fixture sizes, then run the given backend (mutates returned `LayoutData`).
 * Only `'swimlanes'` is supported on this branch; `'domus-orthogonal'` throws.
 */
export declare function parseApplySizesAndLayout(mmdPath: string, sizes: SizesFixture, backendId: LayoutTestBackendId, _options?: {
    trace?: OrthogonalTrace;
}): Promise<LayoutData>;
/** Returns a DOM-free layout runner. `swimlanes` must use `parseApplySizesAndLayout()` (needs fixture sizes mid-pipeline); `domus-orthogonal` throws. */
export declare function getLayoutTestBackend(_id: LayoutTestBackendId): LayoutTestBackend;
export declare function backendsForProfile(profile: DdltFixtureProfile): LayoutTestBackendId[];
