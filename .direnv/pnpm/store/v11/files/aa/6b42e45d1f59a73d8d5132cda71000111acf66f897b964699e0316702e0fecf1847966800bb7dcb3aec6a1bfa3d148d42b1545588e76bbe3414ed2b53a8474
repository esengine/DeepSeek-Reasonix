const require_head = require("../../array/head.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
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
	if (!require_isArrayLike.isArrayLike(arr)) return;
	return require_head.head(require_toArray.toArray(arr));
}
//#endregion
exports.head = head;
