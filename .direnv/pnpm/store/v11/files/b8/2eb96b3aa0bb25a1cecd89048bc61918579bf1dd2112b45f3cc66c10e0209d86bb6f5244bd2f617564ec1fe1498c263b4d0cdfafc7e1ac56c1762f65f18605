//#region src/compat/object/toPairsIn.d.ts
/**
 * Creates an array of key-value pairs from an object, including inherited properties.
 *
 * @template T
 * @param object - The object to query.
 * @returns Returns the array of key-value pairs.
 *
 * @example
 * const object = { a: 1, b: 2 };
 * toPairsIn(object); // [['a', 1], ['b', 2]]
 */
declare function toPairsIn<T>(object?: Record<string, T> | Record<number, T>): Array<[string, T]>;
/**
 * Creates an array of key-value pairs from an object, including inherited properties.
 *
 * @param object - The object to query.
 * @returns Returns the array of key-value pairs.
 *
 * @example
 * const object = { a: 1, b: 2 };
 * toPairsIn(object); // [['a', 1], ['b', 2]]
 */
declare function toPairsIn(object?: object): Array<[string, any]>;
//#endregion
export { toPairsIn };