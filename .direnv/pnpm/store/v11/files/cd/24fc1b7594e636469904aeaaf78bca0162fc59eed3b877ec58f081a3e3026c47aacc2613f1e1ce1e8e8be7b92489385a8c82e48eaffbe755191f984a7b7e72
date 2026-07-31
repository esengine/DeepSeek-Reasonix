import { ListIterateeCustom } from "../_internal/ListIterateeCustom.js";
import { ObjectIterateeCustom } from "../_internal/ObjectIteratee.js";

//#region src/compat/array/some.d.ts
/**
 * Checks if predicate returns truthy for any element of collection.
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @param [predicate] - The function invoked per iteration.
 * @returns Returns `true` if any element passes the predicate check, else `false`.
 *
 * @example
 * some([null, 0, 'yes', false], Boolean);
 * // => true
 */
declare function some<T>(collection: ArrayLike<T> | null | undefined, predicate?: ListIterateeCustom<T, boolean>): boolean;
/**
 * Checks if predicate returns truthy for any element of collection.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param [predicate] - The function invoked per iteration.
 * @returns Returns `true` if any element passes the predicate check, else `false`.
 *
 * @example
 * some({ 'a': 0, 'b': 1, 'c': 0 }, function(n) { return n > 0; });
 * // => true
 */
declare function some<T extends object>(collection: T | null | undefined, predicate?: ObjectIterateeCustom<T, boolean>): boolean;
//#endregion
export { some };