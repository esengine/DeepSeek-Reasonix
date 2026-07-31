//#region src/array/takeRightWhile.d.ts
/**
 * Takes elements from the end of the array while the predicate function returns `true`.
 *
 * @template T - Type of elements in the input array.
 *
 * @param arr - The array to take elements from.
 * @param shouldContinueTaking - The function invoked per element with the item, its index, and the array.
 * @returns A new array containing the elements taken from the end while the predicate returns `true`.
 *
 * @example
 * // Returns [3, 2, 1]
 * takeRightWhile([5, 4, 3, 2, 1], n => n < 4);
 *
 * @example
 * // Returns []
 * takeRightWhile([1, 2, 3], n => n > 3);
 *
 * @example
 * // Using index parameter
 * takeRightWhile([10, 20, 30, 40], (x, index) => index > 1);
 * // Returns: [30, 40]
 */
declare function takeRightWhile<T>(arr: readonly T[], shouldContinueTaking: (item: T, index: number, array: readonly T[]) => boolean): T[];
//#endregion
export { takeRightWhile };