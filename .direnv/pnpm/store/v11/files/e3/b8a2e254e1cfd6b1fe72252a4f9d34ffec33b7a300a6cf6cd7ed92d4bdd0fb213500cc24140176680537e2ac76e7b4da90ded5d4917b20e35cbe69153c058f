//#region src/fp/array/isSubset.d.ts
/**
 * Creates a predicate that checks whether the piped array is a subset of superset.
 *
 * Every value in the piped array must be present in superset, using the main
 * {@link isSubset} equality behavior.
 *
 * @template T - The type of elements in the arrays.
 * @param superset - The array that may contain all values from the piped array.
 * @returns A predicate for the piped array.
 *
 * @example
 * import { isSubset, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2], isSubset([1, 2, 3]));
 * // => true
 */
declare function isSubset<T>(superset: readonly T[]): (array: readonly T[]) => boolean;
//#endregion
export { isSubset };