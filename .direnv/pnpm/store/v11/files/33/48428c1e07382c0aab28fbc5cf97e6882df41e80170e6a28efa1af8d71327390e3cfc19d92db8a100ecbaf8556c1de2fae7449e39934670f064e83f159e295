import { ExtractNestedArrayType } from "../../array/flattenDeep.mjs";

//#region src/fp/array/flattenDeep.d.ts
/**
 * Creates a function that recursively flattens an array.
 *
 * The returned function is lazy-capable inside {@link pipe}. A trailing
 * short-circuiting operator can stop before later nested values are visited.
 *
 * @template T - The type of elements in the array.
 * @returns A function that maps the piped array to a deeply flattened array.
 *
 * @example
 * import { flattenDeep, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, [2, [3]]], flattenDeep());
 * // => [1, 2, 3]
 */
declare function flattenDeep<T>(): (array: readonly T[]) => Array<ExtractNestedArrayType<T>>;
//#endregion
export { flattenDeep };