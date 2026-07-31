import { ListIterator } from "../_internal/ListIterator.js";
import { ObjectIterator } from "../_internal/ObjectIterator.js";
import { ListOfRecursiveArraysOrValues } from "../_internal/ListOfRecursiveArraysOrValues.js";

//#region src/compat/array/flatMapDeep.d.ts
/**
 * Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @returns Returns the new deeply flattened array.
 *
 * @example
 * const obj = { a: [[1, 2]], b: [[[3]]] };
 * flatMapDeep(obj);
 * // => [1, 2, 3]
 */
declare function flatMapDeep<T>(collection: Record<string, ListOfRecursiveArraysOrValues<T> | T> | Record<number, ListOfRecursiveArraysOrValues<T> | T> | null | undefined): T[];
/**
 * Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
 *
 * @template T, R
 * @param collection - The collection to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @returns Returns the new deeply flattened array.
 *
 * @example
 * function duplicate(n) {
 *   return [[[n, n]]];
 * }
 *
 * flatMapDeep([1, 2], duplicate);
 * // => [1, 1, 2, 2]
 */
declare function flatMapDeep<T, R>(collection: ArrayLike<T> | null | undefined, iteratee: ListIterator<T, ListOfRecursiveArraysOrValues<R> | R>): R[];
/**
 * Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
 *
 * @template T, R
 * @param collection - The object to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @returns Returns the new deeply flattened array.
 *
 * @example
 * const obj = { a: 1, b: 2 };
 * flatMapDeep(obj, (value, key) => [[[key, value]]]);
 * // => ['a', 1, 'b', 2]
 */
declare function flatMapDeep<T extends object, R>(collection: T | null | undefined, iteratee: ObjectIterator<T, ListOfRecursiveArraysOrValues<R> | R>): R[];
/**
 * Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
 *
 * @param collection - The collection to iterate over.
 * @param iteratee - The property name to use as iteratee.
 * @returns Returns the new deeply flattened array.
 *
 * @example
 * const users = [
 *   { user: 'barney', hobbies: [['hiking', 'coding']] },
 *   { user: 'fred', hobbies: [['reading']] }
 * ];
 * flatMapDeep(users, 'hobbies');
 * // => ['hiking', 'coding', 'reading']
 */
declare function flatMapDeep(collection: object | null | undefined, iteratee: string): any[];
/**
 * Creates a flattened array of values by running each element through iteratee and recursively flattening the mapped results.
 *
 * @param collection - The collection to iterate over.
 * @param iteratee - The object properties to match.
 * @returns Returns the new deeply flattened array.
 *
 * @example
 * const users = [
 *   { user: 'barney', active: [true, false] },
 *   { user: 'fred', active: [false] }
 * ];
 * flatMapDeep(users, { active: [false] });
 * // => [false]
 */
declare function flatMapDeep(collection: object | null | undefined, iteratee: object): boolean[];
//#endregion
export { flatMapDeep };