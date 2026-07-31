import { ValueIteratee } from "../_internal/ValueIteratee.js";
import { MutableList } from "../_internal/MutableList.js";
import { RejectReadonly } from "../_internal/RejectReadonly.js";

//#region src/compat/array/pullAllBy.d.ts
/**
 * Removes all specified values from an array using an iteratee function.
 *
 * This function changes `arr` in place.
 * If you want to remove values without modifying the original array, use `differenceBy`.
 *
 * @template T
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @param [iteratee] - The iteratee invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1 }, { 'x': 2 }, { 'x': 3 }, { 'x': 1 }];
 *
 * pullAllBy(array, [{ 'x': 1 }, { 'x': 3 }], 'x');
 * console.log(array);
 * // => [{ 'x': 2 }]
 */
declare function pullAllBy<T>(array: T[], values?: ArrayLike<T>, iteratee?: ValueIteratee<T>): T[];
/**
 * Removes all specified values from an array using an iteratee function.
 *
 * This function changes `arr` in place.
 * If you want to remove values without modifying the original array, use `differenceBy`.
 *
 * @template L
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @param [iteratee] - The iteratee invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1 }, { 'x': 2 }, { 'x': 3 }, { 'x': 1 }];
 *
 * pullAllBy(array, [{ 'x': 1 }, { 'x': 3 }], 'x');
 * console.log(array);
 * // => [{ 'x': 2 }]
 */
declare function pullAllBy<L extends MutableList<any>>(array: RejectReadonly<L>, values?: ArrayLike<L[0]>, iteratee?: ValueIteratee<L[0]>): L;
/**
 * Removes all specified values from an array using an iteratee function.
 *
 * This function changes `arr` in place.
 * If you want to remove values without modifying the original array, use `differenceBy`.
 *
 * @template T, U
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @param iteratee - The iteratee invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1 }, { 'x': 2 }, { 'x': 3 }, { 'x': 1 }];
 *
 * pullAllBy(array, [{ 'x': 1 }, { 'x': 3 }], 'x');
 * console.log(array);
 * // => [{ 'x': 2 }]
 */
declare function pullAllBy<T, U>(array: T[], values: ArrayLike<U>, iteratee: ValueIteratee<T | U>): T[];
/**
 * Removes all specified values from an array using an iteratee function.
 *
 * This function changes `arr` in place.
 * If you want to remove values without modifying the original array, use `differenceBy`.
 *
 * @template L, U
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @param iteratee - The iteratee invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1 }, { 'x': 2 }, { 'x': 3 }, { 'x': 1 }];
 *
 * pullAllBy(array, [{ 'x': 1 }, { 'x': 3 }], 'x');
 * console.log(array);
 * // => [{ 'x': 2 }]
 */
declare function pullAllBy<L extends ArrayLike<any>, U>(array: L extends readonly any[] ? never : L, values: ArrayLike<U>, iteratee: ValueIteratee<L[0] | U>): L;
//#endregion
export { pullAllBy };