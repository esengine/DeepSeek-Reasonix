//#region src/compat/array/pull.d.ts
/**
 * Removes all provided values from array using SameValueZero for equality comparisons.
 *
 * **Note:** Unlike `_.without`, this method mutates `array`.
 *
 * @template T
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @returns Returns `array`.
 *
 * @example
 * var array = [1, 2, 3, 1, 2, 3];
 *
 * pull(array, 2, 3);
 * console.log(array);
 * // => [1, 1]
 */
declare function pull<T>(array: T[], ...values: T[]): T[];
/**
 * Removes all provided values from array using SameValueZero for equality comparisons.
 *
 * **Note:** Unlike `_.without`, this method mutates `array`.
 *
 * @template L
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @returns Returns `array`.
 *
 * @example
 * var array = [1, 2, 3, 1, 2, 3];
 *
 * pull(array, 2, 3);
 * console.log(array);
 * // => [1, 1]
 */
declare function pull<L extends ArrayLike<any>>(array: L extends readonly any[] ? never : L, ...values: Array<L[0]>): L;
//#endregion
export { pull };