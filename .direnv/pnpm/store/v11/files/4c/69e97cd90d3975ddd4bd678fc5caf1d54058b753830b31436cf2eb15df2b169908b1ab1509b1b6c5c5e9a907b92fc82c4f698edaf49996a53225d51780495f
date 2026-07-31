import type { VennData } from './vennTypes.js';
import type { DiagramRenderer, DrawDefinition } from '../../diagram-api/types.js';
export declare const draw: DrawDefinition;
/**
 * Ensures that for every N-set union (N \>= 3) in the subset list, all pairwise
 * (2-set) intersections are present in the array passed to the layout engine.
 *
 * The venn.js layout algorithm needs explicit pairwise subset entries to
 * correctly overlap circles when three or more sets share a region. Without them,
 * `union A,B,C["Innovation"]` renders without the shared centre intersection.
 *
 * This function returns a *new* array — the original `subsets` from the DB is never
 * mutated, so all parser/DB tests that assert on `getSubsetData()` continue to pass.
 *
 * The default size for pairwise intersections is chosen as 1/4 of the smaller
 individual set size. This ratio ensures the overlap is visually distinct but
 smaller than either contributing set, maintaining a balanced representation.
 When set sizes are unknown, a fallback of 2.5 (the parser's default for 2-set unions)
 is used.
 */
export declare function ensurePairwiseSubsets(subsets: VennData[]): VennData[];
export declare const renderer: DiagramRenderer;
