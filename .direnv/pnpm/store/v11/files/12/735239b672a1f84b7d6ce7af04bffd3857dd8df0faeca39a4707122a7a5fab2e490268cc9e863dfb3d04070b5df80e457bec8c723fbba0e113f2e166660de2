//#region src/fp/array/zip.d.ts
/**
 * Creates a function that wraps each element of the piped array in a one-item tuple.
 *
 * @template T - The type of elements in the piped array.
 * @returns A function that maps the piped array to one-item tuples.
 *
 * @example
 * import { pipe, zip } from 'es-toolkit/fp';
 *
 * pipe([1, 2], zip());
 * // => [[1], [2]]
 */
declare function zip<T>(): (array: readonly T[]) => Array<[T]>;
/**
 * Creates a function that zips the piped array with one configured array.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the configured array.
 * @param arr2 - The second array to zip.
 * @returns A function that maps the piped array to two-item tuples.
 *
 * @example
 * pipe([1, 2], zip(['a', 'b']));
 * // => [[1, 'a'], [2, 'b']]
 */
declare function zip<T, U>(arr2: readonly U[]): (array: readonly T[]) => Array<[T, U]>;
/**
 * Creates a function that zips the piped array with two configured arrays.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @param arr2 - The second array to zip.
 * @param arr3 - The third array to zip.
 * @returns A function that maps the piped array to three-item tuples.
 */
declare function zip<T, U, V>(arr2: readonly U[], arr3: readonly V[]): (array: readonly T[]) => Array<[T, U, V]>;
/**
 * Creates a function that zips the piped array with three configured arrays.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @template W - The type of elements in the fourth array.
 * @param arr2 - The second array to zip.
 * @param arr3 - The third array to zip.
 * @param arr4 - The fourth array to zip.
 * @returns A function that maps the piped array to four-item tuples.
 */
declare function zip<T, U, V, W>(arr2: readonly U[], arr3: readonly V[], arr4: readonly W[]): (array: readonly T[]) => Array<[T, U, V, W]>;
/**
 * Creates a function that zips the piped array with any number of configured arrays.
 *
 * @template T - The shared element type of the arrays.
 * @param arrs - Additional arrays to zip.
 * @returns A function that maps the piped array to grouped rows.
 */
declare function zip<T>(...arrs: Array<readonly T[]>): (array: readonly T[]) => T[][];
//#endregion
export { zip };