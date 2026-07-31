import { takeRight as takeRight$1 } from "../../array/takeRight.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { toInteger } from "../util/toInteger.mjs";
//#region src/compat/array/takeRight.ts
/**
* Returns a new array containing the last `count` elements from the input array `arr`.
* If `count` is greater than the length of `arr`, the entire array is returned.
*
* @template T - The type of elements in the array.
* @param arr - The array to take elements from.
* @param [count=1] - The number of elements to take.
* @param [guard] - Enables use as an iteratee for methods like `_.map`.
* @returns A new array containing the last `count` elements from `arr`.
*
* @example
* // Returns [4, 5]
* takeRight([1, 2, 3, 4, 5], 2);
*
* @example
* // Returns ['b', 'c']
* takeRight(['a', 'b', 'c'], 2);
*
* @example
* // Returns [1, 2, 3]
* takeRight([1, 2, 3], 5);
*/
function takeRight(arr, count = 1, guard) {
	count = guard ? 1 : toInteger(count);
	if (count <= 0 || !isArrayLike(arr)) return [];
	return takeRight$1(toArray(arr), count);
}
//#endregion
export { takeRight };
