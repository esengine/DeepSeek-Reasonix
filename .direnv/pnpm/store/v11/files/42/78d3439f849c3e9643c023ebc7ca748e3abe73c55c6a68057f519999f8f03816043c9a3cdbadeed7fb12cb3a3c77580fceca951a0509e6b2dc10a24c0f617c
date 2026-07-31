//#region src/math/median.ts
/**
* Calculates the median of an array of numbers.
*
* The median is the middle value of a sorted array.
* If the array has an odd number of elements, the median is the middle value.
* If the array has an even number of elements, it returns the average of the two middle values.
*
* If the array is empty, this function returns `NaN`.
*
* @param nums - An array of numbers to calculate the median.
* @returns The median of all the numbers in the array.
*
* @example
* const arrayWithOddNumberOfElements = [1, 2, 3, 4, 5];
* const result = median(arrayWithOddNumberOfElements);
* // result will be 3
*
* @example
* const arrayWithEvenNumberOfElements = [1, 2, 3, 4];
* const result = median(arrayWithEvenNumberOfElements);
* // result will be 2.5
*/
function median(nums) {
	if (nums.length === 0) return NaN;
	const sorted = nums.slice().sort((a, b) => a - b);
	const middleIndex = Math.floor(sorted.length / 2);
	if (sorted.length % 2 === 0) return (sorted[middleIndex - 1] + sorted[middleIndex]) / 2;
	else return sorted[middleIndex];
}
//#endregion
exports.median = median;
