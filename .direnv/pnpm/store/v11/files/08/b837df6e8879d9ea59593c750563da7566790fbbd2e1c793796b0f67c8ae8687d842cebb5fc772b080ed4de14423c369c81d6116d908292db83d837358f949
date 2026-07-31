//#region src/compat/array/unzipWith.d.ts
/**
 * This method is like `unzip` except that it accepts an iteratee to specify
 * how regrouped values should be combined. The iteratee is invoked with the
 * elements of each group: (...group).
 *
 * @template T, R
 * @param array - The array of grouped elements to process.
 * @param iteratee - The function to combine regrouped values.
 * @returns Returns the new array of regrouped elements.
 *
 * @example
 * unzipWith([[1, 10, 100], [2, 20, 200]], (a, b) => a + b);
 * // => [3, 30, 300]
 */
declare function unzipWith<T, R>(array: ArrayLike<ArrayLike<T>> | null | undefined, iteratee: (...values: T[]) => R): R[];
/**
 * This method is like `unzip` except that it accepts an iteratee to specify
 * how regrouped values should be combined.
 *
 * @template T
 * @param array - The array of grouped elements to process.
 * @returns Returns the new array of regrouped elements.
 *
 * @example
 * unzipWith([[1, 10, 100], [2, 20, 200]]);
 * // => [[1, 2], [10, 20], [100, 200]]
 */
declare function unzipWith<T>(array: ArrayLike<ArrayLike<T>> | null | undefined): T[][];
//#endregion
export { unzipWith };