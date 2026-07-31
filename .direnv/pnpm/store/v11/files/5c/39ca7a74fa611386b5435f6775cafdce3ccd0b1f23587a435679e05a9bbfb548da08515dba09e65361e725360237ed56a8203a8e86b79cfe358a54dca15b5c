const require_uniq = require("./uniq.js");
//#region src/array/union.ts
/**
* Creates an array of unique values from all given arrays.
*
* This function takes two arrays, merges them into a single array, and returns a new array
* containing only the unique values from the merged array.
*
* @template T - The type of elements in the array.
* @param arr1 - The first array to merge and filter for unique values.
* @param arr2 - The second array to merge and filter for unique values.
* @returns A new array of unique values.
*
* @example
* const array1 = [1, 2, 3];
* const array2 = [3, 4, 5];
* const result = union(array1, array2);
* // result will be [1, 2, 3, 4, 5]
*/
function union(arr1, arr2) {
	return require_uniq.uniq(arr1.concat(arr2));
}
//#endregion
exports.union = union;
