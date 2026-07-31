import { MutableList } from "../_internal/MutableList.mjs";
import { RejectReadonly } from "../_internal/RejectReadonly.mjs";

//#region src/compat/array/pullAllWith.d.ts
/**
 * This method is like `_.pullAll` except that it accepts `comparator` which is
 * invoked to compare elements of array to values. The comparator is invoked with
 * two arguments: (arrVal, othVal).
 *
 * **Note:** Unlike `_.differenceWith`, this method mutates `array`.
 *
 * @template T
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @param [comparator] - The comparator invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1, 'y': 2 }, { 'x': 3, 'y': 4 }, { 'x': 5, 'y': 6 }];
 *
 * pullAllWith(array, [{ 'x': 3, 'y': 4 }], _.isEqual);
 * console.log(array);
 * // => [{ 'x': 1, 'y': 2 }, { 'x': 5, 'y': 6 }]
 */
declare function pullAllWith<T>(array: T[], values?: ArrayLike<T>, comparator?: (a: T, b: T) => boolean): T[];
/**
 * This method is like `_.pullAll` except that it accepts `comparator` which is
 * invoked to compare elements of array to values. The comparator is invoked with
 * two arguments: (arrVal, othVal).
 *
 * **Note:** Unlike `_.differenceWith`, this method mutates `array`.
 *
 * @template L
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @param [comparator] - The comparator invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1, 'y': 2 }, { 'x': 3, 'y': 4 }, { 'x': 5, 'y': 6 }];
 *
 * pullAllWith(array, [{ 'x': 3, 'y': 4 }], _.isEqual);
 * console.log(array);
 * // => [{ 'x': 1, 'y': 2 }, { 'x': 5, 'y': 6 }]
 */
declare function pullAllWith<L extends MutableList<any>>(array: RejectReadonly<L>, values?: ArrayLike<L[0]>, comparator?: (a: L[0], b: L[0]) => boolean): L;
/**
 * This method is like `_.pullAll` except that it accepts `comparator` which is
 * invoked to compare elements of array to values. The comparator is invoked with
 * two arguments: (arrVal, othVal).
 *
 * **Note:** Unlike `_.differenceWith`, this method mutates `array`.
 *
 * @template T, U
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @param comparator - The comparator invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1, 'y': 2 }, { 'x': 3, 'y': 4 }, { 'x': 5, 'y': 6 }];
 *
 * pullAllWith(array, [{ 'x': 3, 'y': 4 }], _.isEqual);
 * console.log(array);
 * // => [{ 'x': 1, 'y': 2 }, { 'x': 5, 'y': 6 }]
 */
declare function pullAllWith<T, U>(array: T[], values: ArrayLike<U>, comparator: (a: T, b: U) => boolean): T[];
/**
 * This method is like `_.pullAll` except that it accepts `comparator` which is
 * invoked to compare elements of array to values. The comparator is invoked with
 * two arguments: (arrVal, othVal).
 *
 * **Note:** Unlike `_.differenceWith`, this method mutates `array`.
 *
 * @template L1, L2
 * @param array - The array to modify.
 * @param values - The values to remove.
 * @param comparator - The comparator invoked per element.
 * @returns Returns `array`.
 *
 * @example
 * var array = [{ 'x': 1, 'y': 2 }, { 'x': 3, 'y': 4 }, { 'x': 5, 'y': 6 }];
 *
 * pullAllWith(array, [{ 'x': 3, 'y': 4 }], _.isEqual);
 * console.log(array);
 * // => [{ 'x': 1, 'y': 2 }, { 'x': 5, 'y': 6 }]
 */
declare function pullAllWith<L1 extends MutableList<any>, L2>(array: RejectReadonly<L1>, values: ArrayLike<L2>, comparator: (a: L1[0], b: L2) => boolean): L1;
//#endregion
export { pullAllWith };