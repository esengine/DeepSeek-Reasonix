import { ValueIteratee } from "../_internal/ValueIteratee.js";

//#region src/compat/array/sortedLastIndexBy.d.ts
/**
 * This method is like `sortedLastIndex` except that it accepts `iteratee`
 * which is invoked for `value` and each element of `array` to compute their
 * sort ranking. The iteratee is invoked with one argument: (value).
 *
 * @template T
 * @param array - The sorted array to inspect.
 * @param value - The value to evaluate.
 * @param iteratee - The iteratee invoked per element.
 * @returns Returns the index at which `value` should be inserted into `array`.
 *
 * @example
 * sortedLastIndexBy([{ 'x': 4 }, { 'x': 5 }], { 'x': 4 }, 'x');
 * // => 1
 */
declare function sortedLastIndexBy<T>(array: ArrayLike<T> | null | undefined, value: T, iteratee: ValueIteratee<T>): number;
//#endregion
export { sortedLastIndexBy };