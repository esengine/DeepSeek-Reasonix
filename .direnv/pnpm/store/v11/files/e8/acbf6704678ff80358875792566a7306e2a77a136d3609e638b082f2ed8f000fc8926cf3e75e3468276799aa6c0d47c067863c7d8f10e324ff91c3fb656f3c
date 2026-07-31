import { MemoListIterator } from "../_internal/MemoListIterator.js";
import { MemoObjectIterator } from "../_internal/MemoObjectIterator.js";

//#region src/compat/array/reduceRight.d.ts
/**
 * Reduces an array to a single value using an iteratee function, starting from the right.
 *
 * The `reduceRight()` function goes through each element in an array from right to left and applies a special function (called a "reducer") to them, one by one.
 * This function takes the result of the previous step and the current element to perform a calculation.
 * After going through all the elements, the function gives you one final result.
 *
 * When the `reduceRight()` function starts, there's no previous result to use.
 * If you provide an initial value, it starts with that.
 * If not, it uses the last element of the array and begins with the second to last element for the calculation.
 *
 * The `reduceRight()` function goes through each element in an array from right to left and applies a special function (called a "reducer") to them, one by one.
 * This function takes the result of the previous step and the current element to perform a calculation.
 * After going through all the elements, the function gives you one final result.
 *
 * When the `reduceRight()` function starts, there's no previous result to use.
 * If you provide an initial value, it starts with that.
 * If not, it uses the last element of the array and begins with the second to last element for the calculation.
 *
 * @template T, U
 * @param collection - The array to iterate over.
 * @param callback - The function invoked per iteration.
 * @param accumulator - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight([1, 2, 3], (acc, value) => acc + value, 0);
 * // => 6
 */
declare function reduceRight<T, U>(collection: T[] | null | undefined, callback: MemoListIterator<T, U, T[]>, accumulator: U): U;
/**
 * Reduces an array-like collection to a single value using an iteratee function, starting from the right.
 *
 * @template T, U
 * @param collection - The array-like collection to iterate over.
 * @param callback - The function invoked per iteration.
 * @param accumulator - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight([1, 2, 3], (acc, value) => acc + value, 0);
 * // => 6
 */
declare function reduceRight<T, U>(collection: ArrayLike<T> | null | undefined, callback: MemoListIterator<T, U, ArrayLike<T>>, accumulator: U): U;
/**
 * Reduces an object to a single value using an iteratee function, starting from the right.
 *
 * @template T, U
 * @param collection - The object to iterate over.
 * @param callback - The function invoked per iteration.
 * @param accumulator - The initial value.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight({ a: 1, b: 2, c: 3 }, (acc, value) => acc + value, 0);
 * // => 6
 */
declare function reduceRight<T extends object, U>(collection: T | null | undefined, callback: MemoObjectIterator<T[keyof T], U, T>, accumulator: U): U;
/**
 * Reduces an array to a single value using an iteratee function, starting from the right.
 *
 * The `reduceRight()` function goes through each element in an array from right to left and applies a special function (called a "reducer") to them, one by one.
 * This function takes the result of the previous step and the current element to perform a calculation.
 * After going through all the elements, the function gives you one final result.
 *
 * When the `reduceRight()` function starts, there's no previous result to use.
 * If you provide an initial value, it starts with that.
 * If not, it uses the last element of the array and begins with the second to last element for the calculation.
 *
 * The `reduceRight()` function goes through each element in an array from right to left and applies a special function (called a "reducer") to them, one by one.
 * This function takes the result of the previous step and the current element to perform a calculation.
 * After going through all the elements, the function gives you one final result.
 *
 * When the `reduceRight()` function starts, there's no previous result to use.
 * If you provide an initial value, it starts with that.
 * If not, it uses the last element of the array and begins with the second to last element for the calculation.
 *
 * @template T
 * @param collection - The array to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight([1, 2, 3], (acc, value) => acc + value);
 * // => 6
 */
declare function reduceRight<T>(collection: T[] | null | undefined, callback: MemoListIterator<T, T, T[]>): T | undefined;
/**
 * Reduces an array-like collection to a single value using an iteratee function, starting from the right.
 *
 * @template T
 * @param collection - The array-like collection to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight([1, 2, 3], (acc, value) => acc + value);
 * // => 6
 */
declare function reduceRight<T>(collection: ArrayLike<T> | null | undefined, callback: MemoListIterator<T, T, ArrayLike<T>>): T | undefined;
/**
 * Reduces an object to a single value using an iteratee function, starting from the right.
 *
 * @template T
 * @param collection - The object to iterate over.
 * @param callback - The function invoked per iteration.
 * @returns Returns the accumulated value.
 *
 * @example
 * reduceRight({ a: 1, b: 2, c: 3 }, (acc, value) => acc + value);
 * // => 6
 */
declare function reduceRight<T extends object>(collection: T | null | undefined, callback: MemoObjectIterator<T[keyof T], T[keyof T], T>): T[keyof T] | undefined;
//#endregion
export { reduceRight };