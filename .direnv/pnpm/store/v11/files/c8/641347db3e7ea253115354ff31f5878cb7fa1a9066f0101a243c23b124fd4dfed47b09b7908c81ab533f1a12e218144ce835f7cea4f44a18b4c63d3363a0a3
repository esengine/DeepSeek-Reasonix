const require_zip = require("../../array/zip.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
//#region src/compat/array/zip.ts
/**
* Combines multiple arrays into a single array of tuples.
*
* This function takes multiple arrays and returns a new array where each element is a tuple
* containing the corresponding elements from the input arrays. If the input arrays are of
* different lengths, the resulting array will have the length of the longest input array,
* with undefined values for missing elements.
*
* @template T
* @param arrays - The arrays to zip.
* @returns A new array of tuples containing the corresponding elements from the input arrays.
*
* @example
* const arr1 = [1, 2, 3];
* const arr2 = ['a', 'b', 'c'];
* const arr3 = [true, false];
* const arr4 = [null, null, null];
* const arr5 = [undefined, undefined, undefined];
* const result = zip(arr1, arr2, arr3, arr4, arr5);
* // result will be [[1, 'a', true, null, undefined], [2, 'b', false, null, undefined], [3, 'c', undefined, null, undefined]]
*/
function zip(...arrays) {
	if (!arrays.length) return [];
	return require_zip.zip(...arrays.filter((group) => require_isArrayLikeObject.isArrayLikeObject(group)));
}
//#endregion
exports.zip = zip;
