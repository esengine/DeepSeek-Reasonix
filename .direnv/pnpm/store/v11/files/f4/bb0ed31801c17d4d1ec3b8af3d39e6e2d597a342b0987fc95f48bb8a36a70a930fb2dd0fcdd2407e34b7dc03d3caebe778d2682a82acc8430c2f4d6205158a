const require_differenceBy = require("./differenceBy.js");
const require_intersectionBy = require("./intersectionBy.js");
const require_unionBy = require("./unionBy.js");
//#region src/array/xorBy.ts
/**
* Computes the symmetric difference between two arrays using a custom mapping function.
* The symmetric difference is the set of elements which are in either of the arrays,
* but not in their intersection, determined by the result of the mapping function.
*
* @template T - Type of elements in the input arrays.
* @template U - Type of the values returned by the mapping function.
*
* @param arr1 - The first array.
* @param arr2 - The second array.
* @param mapper - The function to map array elements to comparison values.
* @returns An array containing the elements that are present in either `arr1` or `arr2` but not in both, based on the values returned by the mapping function.
*
* @example
* // Custom mapping function for objects with an 'id' property
* const idMapper = obj => obj.id;
* xorBy([{ id: 1 }, { id: 2 }], [{ id: 2 }, { id: 3 }], idMapper);
* // Returns [{ id: 1 }, { id: 3 }]
*/
function xorBy(arr1, arr2, mapper) {
	const union = require_unionBy.unionBy(arr1, arr2, mapper);
	const intersection = require_intersectionBy.intersectionBy(arr1, arr2, mapper);
	return require_differenceBy.differenceBy(union, intersection, mapper);
}
//#endregion
exports.xorBy = xorBy;
