import { StringIterator } from "../_internal/StringIterator.mjs";
import { ListIterateeCustom } from "../_internal/ListIterateeCustom.mjs";
import { ObjectIterateeCustom } from "../_internal/ObjectIteratee.mjs";

//#region src/compat/array/reject.d.ts
/**
 * Iterates over the collection and rejects elements based on the given predicate.
 * If a function is provided, it is invoked for each element in the collection.
 *
 * @param collection The string to iterate over
 * @param [predicate] The function invoked per iteration
 * @returns Returns a new array of characters that do not satisfy the predicate
 * @example
 * reject('abc', char => char === 'b')
 * // => ['a', 'c']
 */
declare function reject(collection: string | null | undefined, predicate?: StringIterator<boolean>): string[];
/**
 * Iterates over the collection and rejects elements based on the given predicate.
 * If a function is provided, it is invoked for each element in the collection.
 *
 * @template T
 * @param collection The array-like to iterate over
 * @param [predicate] The function invoked per iteration
 * @returns Returns a new array of elements that do not satisfy the predicate
 * @example
 * reject([1, 2, 3], num => num % 2 === 0)
 * // => [1, 3]
 *
 * reject([{ a: 1 }, { a: 2 }, { b: 1 }], 'a')
 * // => [{ b: 1 }]
 */
declare function reject<T>(collection: ArrayLike<T> | null | undefined, predicate?: ListIterateeCustom<T, boolean>): T[];
/**
 * Iterates over the collection and rejects elements based on the given predicate.
 * If a function is provided, it is invoked for each element in the collection.
 *
 * @template T
 * @param collection The object to iterate over
 * @param [predicate] The function invoked per iteration
 * @returns Returns a new array of elements that do not satisfy the predicate
 * @example
 * reject({ a: 1, b: 2, c: 3 }, value => value % 2 === 0)
 * // => [1, 3]
 *
 * reject({ item1: { a: 0, b: true }, item2: { a: 1, b: true }, item3: { a: 2, b: false }}, { b: false })
 * // => [{ a: 0, b: true }, { a: 1, b: true }]
 */
declare function reject<T extends object>(collection: T | null | undefined, predicate?: ObjectIterateeCustom<T, boolean>): Array<T[keyof T]>;
//#endregion
export { reject };