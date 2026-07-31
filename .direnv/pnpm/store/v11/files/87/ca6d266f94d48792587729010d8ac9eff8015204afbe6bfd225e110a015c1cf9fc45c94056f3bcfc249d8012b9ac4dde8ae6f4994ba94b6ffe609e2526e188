//#region src/fp/array/sampleSize.d.ts
/**
 * Creates a function that returns random elements from an array.
 *
 * The returned function follows the main {@link sampleSize} behavior and returns a new array.
 * Use it with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param size - The number of elements to sample.
 * @returns A function that maps a readonly array to sampled elements.
 *
 * @example
 * import { pipe, sampleSize } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4], sampleSize(2));
 * // => two random elements
 */
declare function sampleSize<T>(size: number): (array: readonly T[]) => T[];
//#endregion
export { sampleSize };