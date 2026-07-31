import { difference } from "./difference.mjs";
import { intersection } from "./intersection.mjs";
import { union } from "./union.mjs";
//#region src/array/xor.ts
/**
* Computes the symmetric difference between two arrays. The symmetric difference is the set of elements
* which are in either of the arrays, but not in their intersection.
*
* @template T - The type of elements in the array.
* @param arr1 - The first array.
* @param arr2 - The second array.
* @returns An array containing the elements that are present in either `arr1` or `arr2` but not in both.
*
* @example
* // Returns [1, 2, 5, 6]
* xor([1, 2, 3, 4], [3, 4, 5, 6]);
*
* @example
* // Returns ['a', 'c']
* xor(['a', 'b'], ['b', 'c']);
*/
function xor(arr1, arr2) {
	return difference(union(arr1, arr2), intersection(arr1, arr2));
}
//#endregion
export { xor };
