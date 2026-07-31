//#region src/compat/object/values.d.ts
/**
 * Creates an array of the own enumerable property values of `object`.
 *
 * @template T
 * @param object - The object to query.
 * @returns Returns an array of property values.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * values(obj); // => [1, 2, 3]
 */
declare function values<T>(object: Record<string, T> | Record<number, T> | ArrayLike<T> | null | undefined): T[];
/**
 * Creates an array of the own enumerable property values of `object`.
 *
 * @template T
 * @param object - The object to query.
 * @returns Returns an array of property values.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * values(obj); // => [1, 2, 3]
 */
declare function values<T extends object>(object: T | null | undefined): Array<T[keyof T]>;
/**
 * Creates an array of the own enumerable property values of `object`.
 *
 * @param object - The object to query.
 * @returns Returns an array of property values.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * values(obj); // => [1, 2, 3]
 */
declare function values(object: any): any[];
//#endregion
export { values };