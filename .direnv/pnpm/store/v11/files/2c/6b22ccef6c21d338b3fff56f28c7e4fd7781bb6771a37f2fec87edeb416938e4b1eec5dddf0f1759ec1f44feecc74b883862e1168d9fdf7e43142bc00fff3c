import { MutableList } from "../_internal/MutableList.mjs";
import { RejectReadonly } from "../_internal/RejectReadonly.mjs";

//#region src/compat/array/reverse.d.ts
/**
 * Reverses `array` so that the first element becomes the last, the second element becomes the second to last, and so on.
 *
 * @template L
 * @param array - The array to reverse.
 * @returns Returns `array`.
 *
 * @example
 * const array = [1, 2, 3];
 * reverse(array);
 * // => [3, 2, 1]
 */
declare function reverse<L extends MutableList<any>>(array: RejectReadonly<L>): L;
//#endregion
export { reverse };