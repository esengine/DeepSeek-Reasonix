//#region src/fp/array/chunk.d.ts
/**
 * Creates a function that splits an array into sub-arrays of length `size`. The
 * final chunk holds the remaining elements when the array cannot be divided
 * evenly. Use it with {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param size - The length of each chunk. Must be a positive integer.
 * @returns A function that maps a `readonly T[]` to a `T[][]`.
 * @throws {Error} When `size` is not a positive integer (propagated from the underlying implementation).
 *
 * @example
 * import { pipe, chunk } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 4, 5], chunk(2)); // => [[1, 2], [3, 4], [5]]
 */
declare function chunk<T>(size: number): (array: readonly T[]) => T[][];
//#endregion
export { chunk };