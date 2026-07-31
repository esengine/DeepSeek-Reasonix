import { MutableList } from "../_internal/MutableList.mjs";
import { RejectReadonly } from "../_internal/RejectReadonly.mjs";

//#region src/compat/array/pullAll.d.ts
/**
 * This method is like `_.pull` except that it accepts an array of values to remove.
 *
 * **Note:** Unlike `_.difference`, this method mutates `array`.
 *
 * @template T
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @returns Returns `array`.
 *
 * @example
 * var array = [1, 2, 3, 1, 2, 3];
 *
 * pullAll(array, [2, 3]);
 * console.log(array);
 * // => [1, 1]
 */
declare function pullAll<T>(array: T[], values?: ArrayLike<T>): T[];
/**
 * This method is like `_.pull` except that it accepts an array of values to remove.
 *
 * **Note:** Unlike `_.difference`, this method mutates `array`.
 *
 * @template L
 * @param array - The array to modify.
 * @param [values] - The values to remove.
 * @returns Returns `array`.
 *
 * @example
 * var array = [1, 2, 3, 1, 2, 3];
 *
 * pullAll(array, [2, 3]);
 * console.log(array);
 * // => [1, 1]
 */
declare function pullAll<L extends MutableList<any>>(array: RejectReadonly<L>, values?: ArrayLike<L[0]>): L;
//#endregion
export { pullAll };