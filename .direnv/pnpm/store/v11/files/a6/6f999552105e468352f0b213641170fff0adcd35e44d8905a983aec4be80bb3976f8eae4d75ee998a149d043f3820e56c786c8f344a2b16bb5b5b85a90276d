//#region src/compat/array/sample.d.ts
/**
 * Gets a random element from collection.
 *
 * @template T
 * @param collection - The collection to sample.
 * @returns Returns the random element.
 *
 * @example
 * sample([1, 2, 3, 4]);
 * // => 2
 */
declare function sample<T>(collection: readonly [T, ...T[]]): T;
/**
 * Gets a random element from collection.
 *
 * @template T
 * @param collection - The collection to sample.
 * @returns Returns the random element.
 *
 * @example
 * sample({ 'a': 1, 'b': 2, 'c': 3 });
 * // => 2
 */
declare function sample<T>(collection: Record<string, T> | Record<number, T> | null | undefined): T | undefined;
/**
 * Gets a random element from collection.
 *
 * @template T
 * @param collection - The collection to sample.
 * @returns Returns the random element.
 *
 * @example
 * sample({ 'a': 1, 'b': 2, 'c': 3 });
 * // => 2
 */
declare function sample<T extends object>(collection: T | null | undefined): T[keyof T] | undefined;
//#endregion
export { sample };