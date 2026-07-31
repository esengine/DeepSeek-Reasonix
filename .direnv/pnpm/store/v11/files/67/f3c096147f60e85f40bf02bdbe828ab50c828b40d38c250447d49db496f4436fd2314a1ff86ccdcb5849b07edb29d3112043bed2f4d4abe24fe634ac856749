//#region src/fp/array/uniq.d.ts
/**
 * Creates a function that removes duplicate values from an array, preserving
 * the order of first occurrence. Equality follows SameValueZero, matching
 * Set semantics. Use it with {@link pipe}.
 *
 * The returned function is lazy-capable inside {@link pipe}. It keeps a Set of
 * values already emitted during each pipeline run.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps a readonly array to a duplicate-free array.
 *
 * @example
 * import { pipe, uniq } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 2, 3, 3, 3], uniq());
 * // => [1, 2, 3]
 */
declare function uniq<T>(): (array: readonly T[]) => T[];
//#endregion
export { uniq };