const require_initial = require("../../array/initial.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
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
	if (!require_isArrayLike.isArrayLike(arr)) return [];
	return require_initial.initial(Array.from(arr));
}
//#endregion
exports.initial = initial;
