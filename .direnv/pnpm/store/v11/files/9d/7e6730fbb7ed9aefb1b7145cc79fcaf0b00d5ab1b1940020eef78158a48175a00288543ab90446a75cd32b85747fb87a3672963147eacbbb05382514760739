//#region src/fp/array/zipWith.d.ts
/**
 * Creates a function that maps each value in the piped array with its index.
 *
 * @template T - The type of elements in the piped array.
 * @template R - The type returned by the combiner.
 * @param combine - Called with each value and index.
 * @returns A function that maps the piped array to combined values.
 *
 * @example
 * import { pipe, zipWith } from 'es-toolkit/fp';
 *
 * pipe([1, 2], zipWith((value, index) => value + index));
 * // => [1, 3]
 */
declare function zipWith<T, R>(combine: (item: T, index: number) => R): (array: readonly T[]) => R[];
/**
 * Creates a function that zips the piped array with one configured array and combines each group.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the configured array.
 * @template R - The type returned by the combiner.
 * @param arr2 - The second array to zip.
 * @param combine - Called with a value from each array and the index.
 * @returns A function that maps the piped array to combined values.
 */
declare function zipWith<T, U, R>(arr2: readonly U[], combine: (item1: T, item2: U, index: number) => R): (array: readonly T[]) => R[];
/**
 * Creates a function that zips the piped array with two configured arrays and combines each group.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @template R - The type returned by the combiner.
 * @param arr2 - The second array to zip.
 * @param arr3 - The third array to zip.
 * @param combine - Called with a value from each array and the index.
 * @returns A function that maps the piped array to combined values.
 */
declare function zipWith<T, U, V, R>(arr2: readonly U[], arr3: readonly V[], combine: (item1: T, item2: U, item3: V, index: number) => R): (array: readonly T[]) => R[];
/**
 * Creates a function that zips the piped array with three configured arrays and combines each group.
 *
 * @template T - The type of elements in the piped array.
 * @template U - The type of elements in the second array.
 * @template V - The type of elements in the third array.
 * @template W - The type of elements in the fourth array.
 * @template R - The type returned by the combiner.
 * @param arr2 - The second array to zip.
 * @param arr3 - The third array to zip.
 * @param arr4 - The fourth array to zip.
 * @param combine - Called with a value from each array and the index.
 * @returns A function that maps the piped array to combined values.
 */
declare function zipWith<T, U, V, W, R>(arr2: readonly U[], arr3: readonly V[], arr4: readonly W[], combine: (item1: T, item2: U, item3: V, item4: W, index: number) => R): (array: readonly T[]) => R[];
//#endregion
export { zipWith };