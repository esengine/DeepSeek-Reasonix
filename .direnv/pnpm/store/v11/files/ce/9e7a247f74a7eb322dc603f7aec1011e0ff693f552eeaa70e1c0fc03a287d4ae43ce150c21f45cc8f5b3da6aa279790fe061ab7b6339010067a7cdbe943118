//#region src/array/cartesianProduct.d.ts
/**
 * Computes the [Cartesian product](https://en.wikipedia.org/wiki/Cartesian_product) of the input arrays.
 *
 * @template T
 * @param arr1 - The array to take the product of.
 * @returns An array of single-element tuples.
 */
declare function cartesianProduct<T>(arr1: readonly T[]): Array<[T]>;
/**
 * Computes the [Cartesian product](https://en.wikipedia.org/wiki/Cartesian_product) of the input arrays.
 *
 * @template T, U
 * @param arr1 - The first array to take the product of.
 * @param arr2 - The second array to take the product of.
 * @returns An array of tuples representing the Cartesian product.
 *
 * @example
 * cartesianProduct([1, 2], ['a', 'b']);
 * // => [[1, 'a'], [1, 'b'], [2, 'a'], [2, 'b']]
 */
declare function cartesianProduct<T, U>(arr1: readonly T[], arr2: readonly U[]): Array<[T, U]>;
/**
 * Computes the [Cartesian product](https://en.wikipedia.org/wiki/Cartesian_product) of the input arrays.
 *
 * @template T, U, V
 * @param arr1 - The first array to take the product of.
 * @param arr2 - The second array to take the product of.
 * @param arr3 - The third array to take the product of.
 * @returns An array of tuples representing the Cartesian product.
 */
declare function cartesianProduct<T, U, V>(arr1: readonly T[], arr2: readonly U[], arr3: readonly V[]): Array<[T, U, V]>;
/**
 * Computes the [Cartesian product](https://en.wikipedia.org/wiki/Cartesian_product) of the input arrays.
 *
 * @template T, U, V, W
 * @param arr1 - The first array to take the product of.
 * @param arr2 - The second array to take the product of.
 * @param arr3 - The third array to take the product of.
 * @param arr4 - The fourth array to take the product of.
 * @returns An array of tuples representing the Cartesian product.
 */
declare function cartesianProduct<T, U, V, W>(arr1: readonly T[], arr2: readonly U[], arr3: readonly V[], arr4: readonly W[]): Array<[T, U, V, W]>;
/**
 * Computes the [Cartesian product](https://en.wikipedia.org/wiki/Cartesian_product) of the input arrays.
 *
 * Returns every possible tuple formed by picking one element from each input array, in lexicographic order.
 * The rightmost array advances fastest, like the digits of an odometer.
 *
 * If no arrays are passed, the result is `[[]]` (a single empty tuple).
 * If any input array is empty, the result is `[]`.
 *
 * @template T
 * @param arrs - The arrays to take the product of.
 * @returns An array of tuples representing the Cartesian product.
 *
 * @example
 * cartesianProduct([1, 2], ['a', 'b']);
 * // => [[1, 'a'], [1, 'b'], [2, 'a'], [2, 'b']]
 *
 * @example
 * cartesianProduct([0, 1], [0, 1], [0, 1]);
 * // => [[0,0,0], [0,0,1], [0,1,0], [0,1,1], [1,0,0], [1,0,1], [1,1,0], [1,1,1]]
 *
 * @example
 * cartesianProduct([1, 2, 3], []);
 * // => []
 *
 * @example
 * cartesianProduct();
 * // => [[]]
 */
declare function cartesianProduct<T>(...arrs: Array<readonly T[]>): T[][];
//#endregion
export { cartesianProduct };