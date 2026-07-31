import { intersection as intersection$1 } from "../../array/intersection.mjs";
import { uniq } from "../../array/uniq.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
//#region src/compat/array/intersection.ts
/**
* Returns the intersection of multiple arrays.
*
* This function takes multiple arrays and returns a new array containing the elements that are
* present in all provided arrays. It effectively filters out any elements that are not found
* in every array.
*
* @template T - The type of elements in the arrays.
* @param arrays - The arrays to compare.
* @returns A new array containing the elements that are present in all arrays.
*
* @example
* const array1 = [1, 2, 3, 4, 5];
* const array2 = [3, 4, 5, 6, 7];
* const result = intersection(array1, array2);
* // result will be [3, 4, 5] since these elements are in both arrays.
*/
function intersection(...arrays) {
	if (arrays.length === 0) return [];
	if (!isArrayLikeObject(arrays[0])) return [];
	let result = uniq(Array.from(arrays[0]));
	for (let i = 1; i < arrays.length; i++) {
		const array = arrays[i];
		if (!isArrayLikeObject(array)) return [];
		result = intersection$1(result, Array.from(array));
	}
	return result;
}
//#endregion
export { intersection };
