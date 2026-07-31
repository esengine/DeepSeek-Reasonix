//#region src/fp/array/without.d.ts
/**
 * Creates a function that removes the configured values from an array.
 *
 * The returned function is lazy-capable inside {@link pipe}.
 *
 * @template T - The type of elements in the array.
 * @param values - Values to remove from the piped array.
 * @returns A function that maps the piped array to the remaining values.
 *
 * @example
 * import { pipe, without } from 'es-toolkit/fp';
 *
 * pipe([1, 2, 3, 2], without(2));
 * // => [1, 3]
 */
declare function without<T>(...values: T[]): (array: readonly T[]) => T[];
//#endregion
export { without };