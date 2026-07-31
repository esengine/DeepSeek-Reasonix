//#region src/math/percentile.d.ts
/**
 * Calculates the value at the given percentile of an array of numbers.
 *
 * Sorts the array in ascending order and returns the element at the nearest rank.
 * See {@link https://en.wikipedia.org/wiki/Percentile#The_nearest-rank_method | Nearest rank method}.
 *
 * `NaN` values are sorted as the smallest values. Returns `NaN` if the array is empty.
 *
 * @param arr - An array of numbers to calculate the percentile.
 * @param percentile - The percentile to compute, in the range `[0, 100]`.
 * @returns The value at the given percentile.
 * @throws {Error} Throws an error if `percentile` is not a number, less than `0`, or greater than `100`.
 *
 * @example
 * percentile([1, 2, 3, 4, 5], 50);
 * // Returns 3
 *
 * @example
 * percentile([1, 2, 3, 4, 5], 75);
 * // Returns 4
 *
 * @example
 * percentile([5, 1, 4, 2, 3], 0);
 * // Returns 1
 */
declare function percentile(arr: readonly number[], percentile: number): number;
//#endregion
export { percentile };