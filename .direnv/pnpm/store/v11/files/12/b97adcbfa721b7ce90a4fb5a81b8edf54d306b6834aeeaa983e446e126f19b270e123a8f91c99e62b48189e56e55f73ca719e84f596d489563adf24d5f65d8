import { compact as compact$1 } from "../../array/compact.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/compact.ts
/**
* Removes falsey values (false, null, 0, 0n, '', undefined, NaN) from an array.
*
* @template T - The type of elements in the array.
* @param arr - The input array to remove falsey values.
* @returns A new array with all falsey values removed.
*
* @example
* compact([0, 0n, 1, false, 2, '', 3, null, undefined, 4, NaN, 5]);
* Returns: [1, 2, 3, 4, 5]
*/
function compact(arr) {
	if (!isArrayLike(arr)) return [];
	return compact$1(Array.from(arr));
}
//#endregion
export { compact };
