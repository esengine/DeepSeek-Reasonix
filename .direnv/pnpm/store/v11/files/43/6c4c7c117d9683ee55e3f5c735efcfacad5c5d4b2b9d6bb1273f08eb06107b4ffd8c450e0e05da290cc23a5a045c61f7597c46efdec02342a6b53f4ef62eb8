const require_toFinite = require("../util/toFinite.js");
const require_isIterateeCall = require("../_internal/isIterateeCall.js");
//#region src/compat/math/rangeRight.ts
/**
* Returns an array of numbers from `end` (exclusive) to `start` (inclusive), decrementing by `step`.
*
* @param start - The starting number of the range (inclusive).
* @param end - The end number of the range (exclusive).
* @param step - The step value for the range.
* @returns An array of numbers from `end` (exclusive) to `start` (inclusive) with the specified `step`.
* @throws {Error} Throws an error if the step value is not a non-zero integer.
*
* @example
* // Returns [3, 2, 1, 0]
* rangeRight(4);
*
* @example
* // Returns [-3, -2, -1, 0]
* rangeRight(0, -4, -1);
*/
function rangeRight(start, end, step) {
	if (step && typeof step !== "number" && require_isIterateeCall.isIterateeCall(start, end, step)) end = step = void 0;
	start = require_toFinite.toFinite(start);
	if (end === void 0) {
		end = start;
		start = 0;
	} else end = require_toFinite.toFinite(end);
	step = step === void 0 ? start < end ? 1 : -1 : require_toFinite.toFinite(step);
	const length = Math.max(Math.ceil((end - start) / (step || 1)), 0);
	const result = new Array(length);
	for (let index = length - 1; index >= 0; index--) {
		result[index] = start;
		start += step;
	}
	return result;
}
//#endregion
exports.rangeRight = rangeRight;
