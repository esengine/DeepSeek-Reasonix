const require_pull = require("../../array/pull.js");
//#region src/compat/array/pullAll.ts
/**
* Removes all specified values from an array.
*
* This function changes `arr` in place.
* If you want to remove values without modifying the original array, use `difference`.
*
* @template T
* @param arr - The array to modify.
* @param valuesToRemove - The values to remove from the array.
* @returns The modified array with the specified values removed.
*
* @example
* const numbers = [1, 2, 3, 4, 5, 2, 4];
* pullAll(numbers, [2, 4]);
* console.log(numbers); // [1, 3, 5]
*/
function pullAll(arr, valuesToRemove = []) {
	return require_pull.pull(arr, Array.from(valuesToRemove));
}
//#endregion
exports.pullAll = pullAll;
