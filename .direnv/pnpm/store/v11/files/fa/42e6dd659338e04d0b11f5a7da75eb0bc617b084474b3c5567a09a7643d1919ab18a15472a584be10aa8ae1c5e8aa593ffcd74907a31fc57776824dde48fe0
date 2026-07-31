//#region src/fp/array/intersection.d.ts
/**
 * Creates a function that keeps values from the piped array that are also present in another array.
 *
 * Equality follows SameValueZero through Set membership, matching the main
 * {@link intersection} implementation. The returned function is lazy-capable
 * inside {@link pipe}.
 *
 * @template T - The type of elements in the arrays.
 * @param secondArray - Values to intersect with the piped array.
 * @returns A function that maps the piped array to its intersection.
 *
 * @example
 * import { intersection, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3], intersection([2, 4]));
 * // => [2]
 */
declare function intersection<T>(secondArray: readonly T[]): (array: readonly T[]) => T[];
//#endregion
export { intersection };