const require_sum = require("./sum.js");
//#region src/compat/math/mean.ts
/**
* Calculates the average of an array of numbers.
*
* If the array is empty, this function returns `NaN`.
*
* @param nums - An array of numbers to calculate the average.
* @returns The average of all the numbers in the array.
*
* @example
* const numbers = [1, 2, 3, 4, 5];
* const result = mean(numbers);
* // result will be 3
*/
function mean(nums) {
	const length = nums ? nums.length : 0;
	return length === 0 ? NaN : require_sum.sum(nums) / length;
}
//#endregion
exports.mean = mean;
