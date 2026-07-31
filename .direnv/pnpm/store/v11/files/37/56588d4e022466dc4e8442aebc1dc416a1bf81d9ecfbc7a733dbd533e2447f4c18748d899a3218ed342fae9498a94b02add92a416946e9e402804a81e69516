import { ExtractNestedArrayType } from "../../array/flattenDeep.mjs";

//#region src/fp/array/flatMapDeep.d.ts
/**
 * Creates a function that maps each element and recursively flattens the mapped values.
 *
 * The iteratee receives each value and index. The returned function is lazy-capable inside
 * {@link pipe}; a trailing short-circuiting operator can stop before later input values are mapped.
 *
 * @template T - The type of elements in the input array.
 * @template U - The type returned by the iteratee before recursive flattening.
 * @param iteratee - Called with each value and index to produce values to flatten.
 * @returns A function that maps a readonly array to a deeply flattened array.
 *
 * @example
 * import { flatMapDeep, pipe } from 'es-toolkit/fp';
 *
 * pipe([1, 2], flatMapDeep(value => [[value, value * 10]]));
 * // => [1, 10, 2, 20]
 */
declare function flatMapDeep<T, U>(iteratee: (item: T, index: number) => U): (array: readonly T[]) => Array<ExtractNestedArrayType<U>>;
//#endregion
export { flatMapDeep };