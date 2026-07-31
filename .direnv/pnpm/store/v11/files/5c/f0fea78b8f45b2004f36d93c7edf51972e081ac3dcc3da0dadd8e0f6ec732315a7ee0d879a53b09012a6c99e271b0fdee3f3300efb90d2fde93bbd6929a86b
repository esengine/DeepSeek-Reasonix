import { toFinite } from "../util/toFinite.mjs";
import { isIterateeCall } from "../_internal/isIterateeCall.mjs";
//#region src/compat/math/range.ts
/**
* Returns an array of numbers from `start` (inclusive) to `end` (exclusive), incrementing by `step`.
*
* @param start - The starting number of the range (inclusive).
* @param end - The end number of the range (exclusive).
* @param step - The step value for the range.
* @returns An array of numbers from `start` (inclusive) to `end` (exclusive) with the specified `step`.
*
* @example
* // Returns [0, 1, 2, 3]
* range(4);
*
* @example
* // Returns [0, -1, -2, -3]
* range(0, -4, -1);
*/
function range(start, end, step) {
	if (step && typeof step !== "number" && isIterateeCall(start, end, step)) end = step = void 0;
	start = toFinite(start);
	if (end === void 0) {
		end = start;
		start = 0;
	} else end = toFinite(end);
	step = step === void 0 ? start < end ? 1 : -1 : toFinite(step);
	const length = Math.max(Math.ceil((end - start) / (step || 1)), 0);
	const result = new Array(length);
	for (let index = 0; index < length; index++) {
		result[index] = start;
		start += step;
	}
	return result;
}
//#endregion
export { range };
