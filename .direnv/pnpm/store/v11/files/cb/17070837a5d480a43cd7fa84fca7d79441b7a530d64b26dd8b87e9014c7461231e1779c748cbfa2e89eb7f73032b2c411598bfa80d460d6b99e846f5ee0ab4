import type { LayoutData } from '../../types.js';
import type { LayoutTestBackendId, OrthogonalTrace } from './types.js';
export interface LoadDdltFixtureOptions {
    /** Backend identifier — defaults to `'domus-orthogonal'`. */
    backendId?: LayoutTestBackendId;
    /** Optional pipeline trace sink, populated in place during routing. */
    trace?: OrthogonalTrace;
}
/**
 * Run a DDLT layout for a named fixture using the canonical pipeline.
 *
 * The fixture name is the basename used for both the `.mmd` source and the
 * captured `.sizes.json` under `cypress/platform/dev-diagrams/layout-tests/`.
 * Example: `loadDdltFixture('Company')` reads `Company.mmd` + `Company.sizes.json`.
 *
 * Pipeline used: `parseApplySizesAndLayout` → `runDomusOrthogonalDdlt` (the
 * same pipeline the browser runs in `domus/index.ts:layout()`). Anything that
 * fixes a layout bug in DDLT will fix it in the browser.
 */
export declare function loadDdltFixture(name: string, options?: LoadDdltFixtureOptions): Promise<LayoutData>;
