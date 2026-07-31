const require_uniq = require("../../array/uniq.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/uniq.ts
/**
* Creates a duplicate-free version of an array.
*
* This function takes an array and returns a new array containing only the unique values
* from the original array, preserving the order of first occurrence.
*
* @template T - The type of elements in the array.
* @param arr - The array to process.
* @returns A new array with only unique values from the original array.
*
* @example
* const array = [1, 2, 2, 3, 4, 4, 5];
* const result = uniq(array);
* // result will be [1, 2, 3, 4, 5]
*/
function uniq(arr) {
	if (!require_isArrayLike.isArrayLike(arr)) return [];
	return require_uniq.uniq(Array.from(arr));
}
//#endregion
exports.uniq = uniq;
