import { initial as initial$1 } from "../../array/initial.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/initial.ts
/**
* Returns a new array containing all elements except the last one from the input array.
* If the input array is empty or has only one element, the function returns an empty array.
*
* @template T The type of elements in the array.
* @param arr - The input array.
* @returns A new array containing all but the last element of the input array.
*
* @example
* const arr = [1, 2, 3, 4];
* const result = initial(arr);
* // result will be [1, 2, 3]
*/
function initial(arr) {
	if (!isArrayLike(arr)) return [];
	return initial$1(Array.from(arr));
}
//#endregion
export { initial };
