import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
import { toInteger } from "../util/toInteger.mjs";
//#region src/compat/array/nth.ts
/**
* Gets the element at index `n` of `array`. If `n` is negative, the nth element from the end is returned.
*
* @param array - The array to query.
* @param [n=0] - The index of the element to return.
* @return {T | undefined} Returns the nth element of `array`.
*
* @example
* nth([1, 2, 3], 1); // => 2
* nth([1, 2, 3], -1); // => 3
*/
function nth(array, n = 0) {
	if (!isArrayLikeObject(array) || array.length === 0) return;
	n = toInteger(n);
	if (n < 0) n += array.length;
	return array[n];
}
//#endregion
export { nth };
