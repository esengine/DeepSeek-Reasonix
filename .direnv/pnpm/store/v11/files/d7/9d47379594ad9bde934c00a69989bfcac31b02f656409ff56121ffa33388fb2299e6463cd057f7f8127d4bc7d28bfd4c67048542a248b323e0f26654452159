import { tail as tail$1 } from "../../array/tail.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/tail.ts
/**
* Returns a new array with all elements except for the first.
*
* This function takes an array and returns a new array containing all the elements
* except for the first one. If the input array is empty or has only one element,
* an empty array is returned.
*
* @template T - The type of elements in the array.
* @param arr - The array to get the tail of.
* @returns A new array containing all elements of the input array except for the first one.
*
* @example
* const arr1 = [1, 2, 3];
* const result = tail(arr1);
* // result will be [2, 3]
*
* const arr2 = [1];
* const result2 = tail(arr2);
* // result2 will be []
*
* const arr3 = [];
* const result3 = tail(arr3);
* // result3 will be []
*/
function tail(arr) {
	if (!isArrayLike(arr)) return [];
	return tail$1(toArray(arr));
}
//#endregion
export { tail };
