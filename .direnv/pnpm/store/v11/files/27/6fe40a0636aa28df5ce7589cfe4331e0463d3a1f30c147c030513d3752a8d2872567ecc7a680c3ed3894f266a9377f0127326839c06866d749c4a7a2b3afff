import { ArrayIterator } from "../_internal/ArrayIterator.js";
import { ListIterator } from "../_internal/ListIterator.js";
import { ObjectIterator } from "../_internal/ObjectIterator.js";
import { StringIterator } from "../_internal/StringIterator.js";

//#region src/compat/array/forEachRight.d.ts
/**
 * Iterates over elements of array from right to left and invokes iteratee for each element.
 *
 * @template T
 * @param collection - The array to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns array.
 *
 * @example
 * forEachRight([1, 2], value => console.log(value));
 * // => Logs `2` then `1`.
 */
declare function forEachRight<T>(collection: T[], iteratee?: ArrayIterator<T, any>): T[];
/**
 * Iterates over characters of string from right to left and invokes iteratee for each character.
 *
 * @param collection - The string to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns string.
 *
 * @example
 * forEachRight('abc', char => console.log(char));
 * // => Logs 'c', 'b', then 'a'.
 */
declare function forEachRight(collection: string, iteratee?: StringIterator<any>): string;
/**
 * Iterates over elements of collection from right to left and invokes iteratee for each element.
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns collection.
 *
 * @example
 * forEachRight({ 0: 'a', 1: 'b', length: 2 }, value => console.log(value));
 * // => Logs 'b' then 'a'.
 */
declare function forEachRight<T>(collection: ArrayLike<T>, iteratee?: ListIterator<T, any>): ArrayLike<T>;
/**
 * Iterates over own enumerable string keyed properties of an object from right to left and invokes iteratee for each property.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns object.
 *
 * @example
 * forEachRight({ a: 1, b: 2 }, (value, key) => console.log(key));
 * // => Logs 'b' then 'a'.
 */
declare function forEachRight<T extends object>(collection: T, iteratee?: ObjectIterator<T, any>): T;
/**
 * Iterates over elements of array from right to left and invokes iteratee for each element.
 *
 * @template T, U
 * @param collection - The array to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the array.
 *
 * @example
 * forEachRight([1, 2], value => console.log(value));
 * // => Logs `2` then `1`.
 */
declare function forEachRight<T, U extends T[] | null | undefined>(collection: U & (T[] | null | undefined), iteratee?: ArrayIterator<T, any>): U;
/**
 * Iterates over characters of string from right to left and invokes iteratee for each character.
 *
 * @template T
 * @param collection - The string to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the string.
 *
 * @example
 * forEachRight('abc', char => console.log(char));
 * // => Logs 'c', 'b', then 'a'.
 */
declare function forEachRight<T extends string | null | undefined>(collection: T, iteratee?: StringIterator<any>): T;
/**
 * Iterates over elements of collection from right to left and invokes iteratee for each element.
 *
 * @template T, L
 * @param collection - The collection to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the collection.
 *
 * @example
 * forEachRight({ 0: 'a', 1: 'b', length: 2 }, value => console.log(value));
 * // => Logs 'b' then 'a'.
 */
declare function forEachRight<T, L extends ArrayLike<T> | null | undefined>(collection: L & (ArrayLike<T> | null | undefined), iteratee?: ListIterator<T, any>): L;
/**
 * Iterates over own enumerable string keyed properties of an object from right to left and invokes iteratee for each property.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the object.
 *
 * @example
 * forEachRight({ a: 1, b: 2 }, (value, key) => console.log(key));
 * // => Logs 'b' then 'a'.
 */
declare function forEachRight<T extends object>(collection: T | null | undefined, iteratee?: ObjectIterator<T, any>): T | null | undefined;
//#endregion
export { forEachRight };