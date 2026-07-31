//#region src/compat/array/sampleSize.d.ts
/**
 * Returns a sample element array of a specified size from a collection.
 *
 * @template T
 * @param collection - The collection to sample from.
 * @param [n] - The size of sample.
 * @returns A new array with sample size applied.
 *
 * @example
 * sampleSize([1, 2, 3], 2);
 * // => [2, 3] (example, actual result will vary)
 */
declare function sampleSize<T>(collection: Record<string, T> | Record<number, T> | null | undefined, n?: number): T[];
/**
 * Returns a sample element array of a specified size from an object.
 *
 * @template T
 * @param collection - The object to sample from.
 * @param [n] - The size of sample.
 * @returns A new array with sample size applied.
 *
 * @example
 * sampleSize({ a: 1, b: 2, c: 3 }, 2);
 * // => [2, 3] (example, actual result will vary)
 */
declare function sampleSize<T extends object>(collection: T | null | undefined, n?: number): Array<T[keyof T]>;
//#endregion
export { sampleSize };