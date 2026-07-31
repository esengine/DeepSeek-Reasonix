import { ArrayIterator } from "../_internal/ArrayIterator.mjs";
import { ListIterator } from "../_internal/ListIterator.mjs";
import { ObjectIterator } from "../_internal/ObjectIterator.mjs";
import { StringIterator } from "../_internal/StringIterator.mjs";

//#region src/compat/array/forEach.d.ts
/**
 * Iterates over elements of array and invokes iteratee for each element.
 *
 * @template T
 * @param collection - The array to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns array.
 *
 * @example
 * forEach([1, 2], value => console.log(value));
 * // => Logs `1` then `2`.
 */
declare function forEach<T>(collection: T[], iteratee?: ArrayIterator<T, any>): T[];
/**
 * Iterates over characters of string and invokes iteratee for each character.
 *
 * @param collection - The string to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns string.
 *
 * @example
 * forEach('abc', char => console.log(char));
 * // => Logs 'a', 'b', then 'c'.
 */
declare function forEach(collection: string, iteratee?: StringIterator<any>): string;
/**
 * Iterates over elements of collection and invokes iteratee for each element.
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns collection.
 *
 * @example
 * forEach({ 0: 'a', 1: 'b', length: 2 }, value => console.log(value));
 * // => Logs 'a' then 'b'.
 */
declare function forEach<T>(collection: ArrayLike<T>, iteratee?: ListIterator<T, any>): ArrayLike<T>;
/**
 * Iterates over own enumerable string keyed properties of an object and invokes iteratee for each property.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns object.
 *
 * @example
 * forEach({ a: 1, b: 2 }, (value, key) => console.log(key));
 * // => Logs 'a' then 'b'.
 */
declare function forEach<T extends object>(collection: T, iteratee?: ObjectIterator<T, any>): T;
/**
 * Iterates over elements of array and invokes iteratee for each element.
 *
 * @template T, U
 * @param collection - The array to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the array.
 *
 * @example
 * forEach([1, 2], value => console.log(value));
 * // => Logs `1` then `2`.
 */
declare function forEach<T, U extends T[] | null | undefined>(collection: U & (T[] | null | undefined), iteratee?: ArrayIterator<T, any>): U;
/**
 * Iterates over characters of string and invokes iteratee for each character.
 *
 * @template T
 * @param collection - The string to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the string.
 *
 * @example
 * forEach('abc', char => console.log(char));
 * // => Logs 'a', 'b', then 'c'.
 */
declare function forEach<T extends string | null | undefined>(collection: T, iteratee?: StringIterator<any>): T;
/**
 * Iterates over elements of collection and invokes iteratee for each element.
 *
 * @template T, L
 * @param collection - The collection to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the collection.
 *
 * @example
 * forEach({ 0: 'a', 1: 'b', length: 2 }, value => console.log(value));
 * // => Logs 'a' then 'b'.
 */
declare function forEach<T, L extends ArrayLike<T> | null | undefined>(collection: L & (ArrayLike<T> | null | undefined), iteratee?: ListIterator<T, any>): L;
/**
 * Iterates over own enumerable string keyed properties of an object and invokes iteratee for each property.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param [iteratee] - The function invoked per iteration.
 * @returns Returns the object.
 *
 * @example
 * forEach({ a: 1, b: 2 }, (value, key) => console.log(key));
 * // => Logs 'a' then 'b'.
 */
declare function forEach<T extends object>(collection: T | null | undefined, iteratee?: ObjectIterator<T, any>): T | null | undefined;
//#endregion
export { forEach };