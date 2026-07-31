import { Many } from "../_internal/Many.js";

//#region src/compat/object/omit.d.ts
/**
 * Creates a new object with specified keys omitted.
 *
 * @template T - The type of object.
 * @template K - The type of keys to omit.
 * @param object - The object to omit keys from.
 * @param paths - The keys to be omitted from the object.
 * @returns A new object with the specified keys omitted.
 *
 * @example
 * omit({ a: 1, b: 2, c: 3 }, 'a', 'c');
 * // => { b: 2 }
 */
declare function omit<T extends object, K extends PropertyKey[]>(object: T | null | undefined, ...paths: K): Pick<T, Exclude<keyof T, K[number]>>;
/**
 * Creates a new object with specified keys omitted.
 *
 * @template T - The type of object.
 * @template K - The type of keys to omit.
 * @param object - The object to omit keys from.
 * @param paths - The keys to be omitted from the object.
 * @returns A new object with the specified keys omitted.
 *
 * @example
 * omit({ a: 1, b: 2, c: 3 }, 'a', ['b', 'c']);
 * // => {}
 */
declare function omit<T extends object, K extends keyof T>(object: T | null | undefined, ...paths: Array<Many<K>>): Omit<T, K>;
/**
 * Creates a new object with specified keys omitted.
 *
 * @template T - The type of object.
 * @param object - The object to omit keys from.
 * @param paths - The keys to be omitted from the object.
 * @returns A new object with the specified keys omitted.
 *
 * @example
 * omit({ a: 1, b: 2, c: 3 }, 'a', 'b');
 * // => { c: 3 }
 */
declare function omit<T extends object>(object: T | null | undefined, ...paths: Array<Many<PropertyKey>>): Partial<T>;
//#endregion
export { omit };