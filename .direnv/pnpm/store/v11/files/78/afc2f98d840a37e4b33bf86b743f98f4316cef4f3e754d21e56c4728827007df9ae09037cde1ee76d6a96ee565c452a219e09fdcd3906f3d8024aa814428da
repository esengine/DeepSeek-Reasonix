//#region src/compat/object/fromPairs.d.ts
type PropertyName = string | number | symbol;
/**
 * Converts an array of key-value pairs into an object.
 *
 * @template T - The type of the values.
 * @param pairs - An array of key-value pairs.
 * @returns An object where keys are strings and values are of type T.
 *
 * @example
 * const pairs = [['a', 1], ['b', 2]];
 * const result = fromPairs(pairs);
 * // => { a: 1, b: 2 }
 */
declare function fromPairs<T>(pairs: ArrayLike<[PropertyName, T]> | null | undefined): Record<string, T>;
/**
 * Converts an array of key-value pairs into an object.
 *
 * @param pairs - An array of key-value pairs.
 * @returns An object where keys are strings and values can be any type.
 *
 * @example
 * const pairs = [['a', 1], ['b', 'hello']];
 * const result = fromPairs(pairs);
 * // => { a: 1, b: 'hello' }
 */
declare function fromPairs(pairs: ArrayLike<any[]> | null | undefined): Record<string, any>;
//#endregion
export { fromPairs };