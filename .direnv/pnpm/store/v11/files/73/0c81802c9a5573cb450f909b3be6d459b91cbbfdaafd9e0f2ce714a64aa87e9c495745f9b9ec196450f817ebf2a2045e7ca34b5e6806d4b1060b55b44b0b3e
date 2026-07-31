//#region src/math/clamp.ts
/**
* Clamps a number within the specified bounds.
*
* This function takes a number and one or two bounds, and returns the number clamped within the specified bounds.
* If only one bound is provided, it returns the minimum of the value and the bound.
*
* @param value - The number to clamp.
* @param bound1 - The minimum bound to clamp the number, or the maximum bound if bound2 is not provided.
* @param [bound2] - The maximum bound to clamp the number. If not provided, the function will only consider bound1 as the upper limit.
* @returns The clamped number within the specified bounds.
*
* @example
* const result1 = clamp(10, 5); // result1 will be 5, as 10 is clamped to the bound 5
* const result2 = clamp(10, 5, 15); // result2 will be 10, as it is within the bounds 5 and 15
* const result3 = clamp(2, 5, 15); // result3 will be 5, as 2 is clamped to the lower bound 5
* const result4 = clamp(20, 5, 15); // result4 will be 15, as 20 is clamped to the upper bound 15
*/
function clamp(value, bound1, bound2) {
	if (bound2 == null) return Math.min(value, bound1);
	return Math.min(Math.max(value, bound1), bound2);
}
//#endregion
exports.clamp = clamp;
