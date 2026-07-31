import { toFinite } from "../util/toFinite.mjs";
import { isIterateeCall } from "../_internal/isIterateeCall.mjs";
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
	if (step && typeof step !== "number" && isIterateeCall(start, end, step)) end = step = void 0;
	start = toFinite(start);
	if (end === void 0) {
		end = start;
		start = 0;
	} else end = toFinite(end);
	step = step === void 0 ? start < end ? 1 : -1 : toFinite(step);
	const length = Math.max(Math.ceil((end - start) / (step || 1)), 0);
	const result = new Array(length);
	for (let index = length - 1; index >= 0; index--) {
		result[index] = start;
		start += step;
	}
	return result;
}
//#endregion
export { rangeRight };
