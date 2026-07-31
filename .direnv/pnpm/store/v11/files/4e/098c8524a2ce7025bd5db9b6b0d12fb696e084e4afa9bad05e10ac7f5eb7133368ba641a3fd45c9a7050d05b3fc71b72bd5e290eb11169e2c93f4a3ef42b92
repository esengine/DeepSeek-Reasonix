const require_difference = require("./difference.js");
const require_intersection = require("./intersection.js");
const require_union = require("./union.js");
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
	return require_difference.difference(require_union.union(arr1, arr2), require_intersection.intersection(arr1, arr2));
}
//#endregion
exports.xor = xor;
