//#region src/fp/array/flatten.d.ts
/**
 * Creates a function that flattens an array up to the specified depth.
 *
 * The returned function is lazy-capable inside {@link pipe}. A trailing
 * short-circuiting operator can stop before later nested values are visited.
 *
 * @template T - The type of elements in the array.
 * @template D - The depth to which nested arrays should be flattened.
 * @param depth - The flattening depth. Defaults to 1.
 * @returns A function that maps the piped array to a flattened array.
 *
 * @example
 * import { flatten, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, [2, [3]]], flatten(2));
 * // => [1, 2, 3]
 */
declare function flatten<T, D extends number = 1>(depth?: D): (array: readonly T[]) => Array<FlatArray<T[], D>>;
//#endregion
export { flatten };