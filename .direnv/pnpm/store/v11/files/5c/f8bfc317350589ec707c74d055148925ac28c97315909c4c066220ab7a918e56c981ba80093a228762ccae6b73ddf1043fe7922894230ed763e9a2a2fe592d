import { head as head$1 } from "../../array/head.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
//#region src/compat/array/head.ts
/**
* Returns the first element of an array or `undefined` if the array is empty.
*
* This function takes an array and returns the first element of the array.
* If the array is empty, the function returns `undefined`.
*
* @template T - The type of elements in the array.
* @param arr - The array from which to get the first element.
* @returns The first element of the array, or `undefined` if the array is empty.
*
* @example
* const emptyArr: number[] = [];
* const noElement = head(emptyArr);
* // noElement will be undefined
*/
function head(arr) {
	if (!isArrayLike(arr)) return;
	return head$1(toArray(arr));
}
//#endregion
export { head };
