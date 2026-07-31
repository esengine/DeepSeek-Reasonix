const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_toInteger = require("../util/toInteger.js");
const require_isIterateeCall = require("../_internal/isIterateeCall.js");
//#region src/compat/array/slice.ts
/**
* Create a slice of `array` from `start` up to, but not including, `end`.
*
* It does not return a dense array for sparse arrays unlike the native `Array.prototype.slice`.
*
* @template T - The type of the array elements.
* @param array - The array to slice.
* @param [start=0] - The start position.
* @param [end=array.length] - The end position.
* @returns Returns the slice of `array`.
*
* @example
* slice([1, 2, 3], 1, 2); // => [2]
* slice(new Array(3)); // => [undefined, undefined, undefined]
*/
function slice(array, start, end) {
	if (!require_isArrayLike.isArrayLike(array)) return [];
	const length = array.length;
	if (end === void 0) end = length;
	else if (typeof end !== "number" && require_isIterateeCall.isIterateeCall(array, start, end)) {
		start = 0;
		end = length;
	}
	start = require_toInteger.toInteger(start);
	end = require_toInteger.toInteger(end);
	if (start < 0) start = Math.max(length + start, 0);
	else start = Math.min(start, length);
	if (end < 0) end = Math.max(length + end, 0);
	else end = Math.min(end, length);
	const resultLength = Math.max(end - start, 0);
	const result = new Array(resultLength);
	for (let i = 0; i < resultLength; ++i) result[i] = array[start + i];
	return result;
}
//#endregion
exports.slice = slice;
