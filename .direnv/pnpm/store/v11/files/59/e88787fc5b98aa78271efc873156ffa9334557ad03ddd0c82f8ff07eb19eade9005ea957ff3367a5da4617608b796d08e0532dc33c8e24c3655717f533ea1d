import { MemoListIterator } from "../_internal/MemoListIterator.js";
import { MemoObjectIterator } from "../_internal/MemoObjectIterator.js";

//#region src/compat/array/reduce.d.ts
/**
 * Reduces an array to a single value using an iteratee function.
 *
 * @param collection - The array to iterate over
 * @param callback - The function invoked per iteration
 * @param accumulator - The initial value
 * @returns Returns the accumulated value
 *
 * @example
 * const array = [1, 2, 3];
 * reduce(array, (acc, value) => acc + value, 0); // => 6
 */
declare function reduce<T, U>(collection: T[] | null | undefined, callback: MemoListIterator<T, U, T[]>, accumulator: U): U;
/**
 * Reduces an array-like object to a single value using an iteratee function.
 *
 * @param collection - The array-like object to iterate over
 * @param callback - The function invoked per iteration
 * @param accumulator - The initial value
 * @returns Returns the accumulated value
 *
 * @example
 * const arrayLike = {0: 1, 1: 2, 2: 3, length: 3};
 * reduce(arrayLike, (acc, value) => acc + value, 0); // => 6
 */
declare function reduce<T, U>(collection: ArrayLike<T> | null | undefined, callback: MemoListIterator<T, U, ArrayLike<T>>, accumulator: U): U;
/**
 * Reduces an object to a single value using an iteratee function.
 *
 * @param collection - The object to iterate over
 * @param callback - The function invoked per iteration
 * @param accumulator - The initial value
 * @returns Returns the accumulated value
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * reduce(obj, (acc, value) => acc + value, 0); // => 6
 */
declare function reduce<T extends object, U>(collection: T | null | undefined, callback: MemoObjectIterator<T[keyof T], U, T>, accumulator: U): U;
/**
 * Reduces an array to a single value using an iteratee function.
 *
 * @param collection - The array to iterate over
 * @param callback - The function invoked per iteration
 * @returns Returns the accumulated value
 *
 * @example
 * const array = [1, 2, 3];
 * reduce(array, (acc, value) => acc + value); // => 6
 */
declare function reduce<T>(collection: T[] | null | undefined, callback: MemoListIterator<T, T, T[]>): T | undefined;
/**
 * Reduces an array-like object to a single value using an iteratee function.
 *
 * @param collection - The array-like object to iterate over
 * @param callback - The function invoked per iteration
 * @returns Returns the accumulated value
 *
 * @example
 * const arrayLike = {0: 1, 1: 2, 2: 3, length: 3};
 * reduce(arrayLike, (acc, value) => acc + value); // => 6
 */
declare function reduce<T>(collection: ArrayLike<T> | null | undefined, callback: MemoListIterator<T, T, ArrayLike<T>>): T | undefined;
/**
 * Reduces an object to a single value using an iteratee function.
 *
 * @param collection - The object to iterate over
 * @param callback - The function invoked per iteration
 * @returns Returns the accumulated value
 *
 * @example
 * const obj = { a: 1, b: 2, c: 3 };
 * reduce(obj, (acc, value) => acc + value); // => 6
 */
declare function reduce<T extends object>(collection: T | null | undefined, callback: MemoObjectIterator<T[keyof T], T[keyof T], T>): T[keyof T] | undefined;
//#endregion
export { reduce };