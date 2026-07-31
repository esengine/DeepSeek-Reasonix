import { ValueIteratee } from "../_internal/ValueIteratee.mjs";

//#region src/compat/array/intersectionBy.d.ts
/**
 * Creates an array of unique values that are included in all given arrays, using an iteratee to compute equality.
 *
 * @template T, U
 * @param array - The array to inspect.
 * @param values - The values to compare.
 * @param iteratee - The iteratee invoked per element.
 * @returns Returns the new array of intersecting values.
 *
 * @example
 * intersectionBy([2.1, 1.2], [2.3, 3.4], Math.floor);
 * // => [2.1]
 */
declare function intersectionBy<T, U>(array: ArrayLike<T> | null, values: ArrayLike<U>, iteratee: ValueIteratee<T | U>): T[];
/**
 * Creates an array of unique values that are included in all given arrays, using an iteratee to compute equality.
 *
 * @template T, U, V
 * @param array - The array to inspect.
 * @param values1 - The first values to compare.
 * @param values2 - The second values to compare.
 * @param iteratee - The iteratee invoked per element.
 * @returns Returns the new array of intersecting values.
 *
 * @example
 * intersectionBy([2.1, 1.2], [2.3, 3.4], [2.5], Math.floor);
 * // => [2.1]
 */
declare function intersectionBy<T, U, V>(array: ArrayLike<T> | null, values1: ArrayLike<U>, values2: ArrayLike<V>, iteratee: ValueIteratee<T | U | V>): T[];
/**
 * Creates an array of unique values that are included in all given arrays, using an iteratee to compute equality.
 *
 * @template T, U, V, W
 * @param array - The array to inspect.
 * @param values1 - The first values to compare.
 * @param values2 - The second values to compare.
 * @param values - The other arrays to compare, and the iteratee to use.
 * @returns Returns the new array of intersecting values.
 *
 * @example
 * intersectionBy([2.1, 1.2], [2.3, 3.4], [2.5], [2.6, 1.7], Math.floor);
 * // => [2.1]
 */
declare function intersectionBy<T, U, V, W>(array: ArrayLike<T> | null | undefined, values1: ArrayLike<U>, values2: ArrayLike<V>, ...values: Array<ArrayLike<W> | ValueIteratee<T | U | V | W>>): T[];
/**
 * Creates an array of unique values that are included in all given arrays.
 *
 * @template T
 * @param [array] - The array to inspect.
 * @param values - The values to compare.
 * @returns Returns the new array of intersecting values.
 *
 * @example
 * intersectionBy([2, 1], [2, 3]);
 * // => [2]
 */
declare function intersectionBy<T>(array?: ArrayLike<T> | null, ...values: Array<ArrayLike<T>>): T[];
/**
 * Creates an array of unique values that are included in all given arrays, using an iteratee to compute equality.
 *
 * @template T
 * @param values - The arrays to compare and the iteratee to use.
 * @returns Returns the new array of intersecting values.
 *
 * @example
 * intersectionBy([2.1, 1.2], [2.3, 3.4], Math.floor);
 * // => [2.1]
 */
declare function intersectionBy<T>(...values: Array<ArrayLike<T> | ValueIteratee<T>>): T[];
//#endregion
export { intersectionBy };