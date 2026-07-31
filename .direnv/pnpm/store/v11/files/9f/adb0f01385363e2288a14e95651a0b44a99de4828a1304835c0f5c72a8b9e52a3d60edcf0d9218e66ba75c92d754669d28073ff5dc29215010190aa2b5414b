import { ListIterator } from "../_internal/ListIterator.js";
import { ObjectIterator } from "../_internal/ObjectIterator.js";
import { ListOfRecursiveArraysOrValues } from "../_internal/ListOfRecursiveArraysOrValues.js";

//#region src/compat/array/flatMapDepth.d.ts
/**
 * Creates a flattened array of values by running each element through iteratee and flattening the mapped results up to depth times.
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @returns Returns the new flattened array.
 *
 * @example
 * const obj = { a: [[1, 2]], b: [[[3]]] };
 * flatMapDepth(obj);
 * // => [1, 2, [3]]
 */
declare function flatMapDepth<T>(collection: Record<string, ListOfRecursiveArraysOrValues<T> | T> | Record<number, ListOfRecursiveArraysOrValues<T> | T> | null | undefined): T[];
/**
 * Creates a flattened array of values by running each element through iteratee and flattening the mapped results up to depth times.
 *
 * @template T, R
 * @param collection - The collection to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @param [depth=1] - The maximum recursion depth.
 * @returns Returns the new flattened array.
 *
 * @example
 * function duplicate(n) {
 *   return [[n, n]];
 * }
 *
 * flatMapDepth([1, 2], duplicate, 2);
 * // => [1, 1, 2, 2]
 */
declare function flatMapDepth<T, R>(collection: ArrayLike<T> | null | undefined, iteratee: ListIterator<T, ListOfRecursiveArraysOrValues<R> | R>, depth?: number): R[];
/**
 * Creates a flattened array of values by running each element through iteratee and flattening the mapped results up to depth times.
 *
 * @template T, R
 * @param collection - The object to iterate over.
 * @param iteratee - The function invoked per iteration.
 * @param [depth=1] - The maximum recursion depth.
 * @returns Returns the new flattened array.
 *
 * @example
 * const obj = { a: 1, b: 2 };
 * flatMapDepth(obj, (value, key) => [[key, value]], 2);
 * // => ['a', 1, 'b', 2]
 */
declare function flatMapDepth<T extends object, R>(collection: T | null | undefined, iteratee: ObjectIterator<T, ListOfRecursiveArraysOrValues<R> | R>, depth?: number): R[];
/**
 * Creates a flattened array of values by running each element through iteratee and flattening the mapped results up to depth times.
 *
 * @param collection - The collection to iterate over.
 * @param iteratee - The property name to use as iteratee.
 * @param [depth=1] - The maximum recursion depth.
 * @returns Returns the new flattened array.
 *
 * @example
 * const users = [
 *   { user: 'barney', hobbies: [['hiking'], ['coding']] },
 *   { user: 'fred', hobbies: [['reading']] }
 * ];
 * flatMapDepth(users, 'hobbies', 2);
 * // => ['hiking', 'coding', 'reading']
 */
declare function flatMapDepth(collection: object | null | undefined, iteratee: string, depth?: number): any[];
/**
 * Creates a flattened array of values by running each element through iteratee and flattening the mapped results up to depth times.
 *
 * @param collection - The collection to iterate over.
 * @param iteratee - The object properties to match.
 * @param [depth=1] - The maximum recursion depth.
 * @returns Returns the new flattened array.
 *
 * @example
 * const users = [
 *   { user: 'barney', active: [[true], [false]] },
 *   { user: 'fred', active: [[false]] }
 * ];
 * flatMapDepth(users, { active: [[false]] });
 * // => [false]
 */
declare function flatMapDepth(collection: object | null | undefined, iteratee: object, depth?: number): boolean[];
//#endregion
export { flatMapDepth };