//#region src/math/range.ts
/**
* Returns an array of numbers from `start` (inclusive) to `end` (exclusive), incrementing by `step`.
*
* @param start - The starting number of the range (inclusive).
* @param end - The end number of the range (exclusive).
* @param step - The step value for the range.
* @returns An array of numbers from `start` (inclusive) to `end` (exclusive) with the specified `step`.
* @throws {Error} Throws an error if the step value is not a non-zero integer.
*
* @example
* // Returns [0, 1, 2, 3]
* range(4);
*
* @example
* // Returns [0, -1, -2, -3]
* range(0, -4, -1);
*/
function range(start, end, step = 1) {
	if (end == null) {
		end = start;
		start = 0;
	}
	if (!Number.isInteger(step) || step === 0) throw new Error(`The step value must be a non-zero integer.`);
	const length = Math.max(Math.ceil((end - start) / step), 0);
	const result = new Array(length);
	for (let i = 0; i < length; i++) result[i] = start + i * step;
	return result;
}
//#endregion
export { range };
