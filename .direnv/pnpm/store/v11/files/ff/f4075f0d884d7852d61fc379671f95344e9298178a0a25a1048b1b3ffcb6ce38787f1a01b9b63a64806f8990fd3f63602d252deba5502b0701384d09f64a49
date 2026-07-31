//#region src/math/rangeRight.ts
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
function rangeRight(start, end, step = 1) {
	if (end == null) {
		end = start;
		start = 0;
	}
	if (!Number.isInteger(step) || step === 0) throw new Error(`The step value must be a non-zero integer.`);
	const length = Math.max(Math.ceil((end - start) / step), 0);
	const result = new Array(length);
	for (let i = 0; i < length; i++) result[i] = start + (length - i - 1) * step;
	return result;
}
//#endregion
export { rangeRight };
