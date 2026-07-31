//#region src/fp/array/cartesianProduct.d.ts
/**
 * Creates a function that wraps each element of the piped array in a one-item tuple.
 *
 * @template T - The type of elements in the piped array.
 * @returns A function that maps the piped array to one-item cartesian product tuples.
 *
 * @example
 * import { cartesianProduct, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2], cartesianProduct());
 * // => [[1], [2]]
 */
declare function cartesianProduct<T>(): (array: readonly T[]) => Array<[T]>;
/**
 * Creates a function that computes the cartesian product of the piped array and one configured array.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the configured array.
 * @param arr2 - The second array in the cartesian product.
 * @returns A function that maps the piped array to two-item cartesian product tuples.
 *
 * @example
 * pipe([1, 2], cartesianProduct(['a', 'b']));
 * // => [[1, 'a'], [1, 'b'], [2, 'a'], [2, 'b']]
 */
declare function cartesianProduct<T, U>(arr2: readonly U[]): (array: readonly T[]) => Array<[T, U]>;
/**
 * Creates a function that computes the cartesian product of the piped array and two configured arrays.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @param arr2 - The second array in the cartesian product.
 * @param arr3 - The third array in the cartesian product.
 * @returns A function that maps the piped array to three-item cartesian product tuples.
 */
declare function cartesianProduct<T, U, V>(arr2: readonly U[], arr3: readonly V[]): (array: readonly T[]) => Array<[T, U, V]>;
/**
 * Creates a function that computes the cartesian product of the piped array and three configured arrays.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @template W - The type of elements in the fourth array.
 * @param arr2 - The second array in the cartesian product.
 * @param arr3 - The third array in the cartesian product.
 * @param arr4 - The fourth array in the cartesian product.
 * @returns A function that maps the piped array to four-item cartesian product tuples.
 */
declare function cartesianProduct<T, U, V, W>(arr2: readonly U[], arr3: readonly V[], arr4: readonly W[]): (array: readonly T[]) => Array<[T, U, V, W]>;
/**
 * Creates a function that computes the cartesian product of the piped array and any number of configured arrays.
 *
 * @template T - The shared element type of the arrays.
 * @param arrs - Additional arrays included in the cartesian product.
 * @returns A function that maps the piped array to cartesian product rows.
 */
declare function cartesianProduct<T>(...arrs: Array<readonly T[]>): (array: readonly T[]) => T[][];
//#endregion
export { cartesianProduct };