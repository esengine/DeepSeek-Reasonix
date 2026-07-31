//#region src/object/omit.d.ts
/**
 * Creates a new object with specified keys omitted.
 *
 * This function takes an object and an array of keys, and returns a new object that
 * excludes the properties corresponding to the specified keys.
 *
 * @template T - The type of object.
 * @template K - The type of keys in object.
 * @param obj - The object to omit keys from.
 * @param keys - An array of keys to be omitted from the object.
 * @returns A new object with the specified keys omitted.
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * const result = omit(obj, ['b', 'c']);
 * // result will be { a: 1 }
 */
declare function omit<T extends Record<string, any>, K extends keyof T>(obj: T, keys: readonly K[]): Omit<T, K>;
//#endregion
export { omit };