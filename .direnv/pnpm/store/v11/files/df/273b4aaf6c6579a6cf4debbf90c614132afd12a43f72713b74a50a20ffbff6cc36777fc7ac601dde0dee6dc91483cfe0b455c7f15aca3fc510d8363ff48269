import { ValueIteratee } from "../_internal/ValueIteratee.mjs";
import { ValueIteratorTypeGuard } from "../_internal/ValueIteratorTypeGuard.mjs";

//#region src/compat/array/partition.d.ts
/**
 * Creates an array of elements split into two groups, the first of which contains elements
 * predicate returns truthy for, while the second of which contains elements predicate returns falsey for.
 * The predicate is invoked with one argument: (value).
 *
 * @template T, U
 * @param collection - The collection to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the array of grouped elements.
 *
 * @example
 * partition([1, 2, 3, 4], n => n % 2 === 0);
 * // => [[2, 4], [1, 3]]
 */
declare function partition<T, U extends T>(collection: ArrayLike<T> | null | undefined, callback: ValueIteratorTypeGuard<T, U>): [U[], Array<Exclude<T, U>>];
/**
 * Creates an array of elements split into two groups, the first of which contains elements
 * predicate returns truthy for, while the second of which contains elements predicate returns falsey for.
 * The predicate is invoked with one argument: (value).
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the array of grouped elements.
 *
 * @example
 * partition([1, 2, 3, 4], n => n % 2 === 0);
 * // => [[2, 4], [1, 3]]
 */
declare function partition<T>(collection: ArrayLike<T> | null | undefined, callback: ValueIteratee<T>): [T[], T[]];
/**
 * Creates an array of elements split into two groups, the first of which contains elements
 * predicate returns truthy for, while the second of which contains elements predicate returns falsey for.
 * The predicate is invoked with one argument: (value).
 *
 * @template T
 * @param collection - The collection to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the array of grouped elements.
 *
 * @example
 * partition({ a: 1, b: 2, c: 3 }, n => n % 2 === 0);
 * // => [[2], [1, 3]]
 */
declare function partition<T extends object>(collection: T | null | undefined, callback: ValueIteratee<T[keyof T]>): [Array<T[keyof T]>, Array<T[keyof T]>];
//#endregion
export { partition };