//#region src/array/windowed.ts
/**
* Creates an array of sub-arrays (windows) from the input array, each of the specified size.
* The windows can overlap depending on the step size provided.
*
* By default, only full windows are included in the result, and any leftover elements that can't form a full window are ignored.
*
* If the `partialWindows` option is set to true in the options object, the function will also include partial windows at the end of the result.
* Partial windows are smaller sub-arrays created when there aren't enough elements left in the input array to form a full window.
*
* @template T
* @param arr - The input array to create windows from.
* @param size - The size of each window. Must be a positive integer.
* @param [step=1] - The step size between the start of each window. Must be a positive integer.
* @param [options={}] - Options object to configure the behavior of the function.
* @param [options.partialWindows=false] - Whether to include partial windows at the end of the array.
* @returns An array of windows (sub-arrays) created from the input array.
* @throws {Error} If the size or step is not a positive integer.
*
* @example
* windowed([1, 2, 3, 4], 2);
* // => [[1, 2], [2, 3], [3, 4]]
*
* @example
* windowed([1, 2, 3, 4, 5, 6], 3, 2);
* // => [[1, 2, 3], [3, 4, 5]]
*
* @example
* windowed([1, 2, 3, 4, 5, 6], 3, 2, { partialWindows: true });
* // => [[1, 2, 3], [3, 4, 5], [5, 6]]
*/
function windowed(arr, size, step = 1, { partialWindows = false } = {}) {
	if (size <= 0 || !Number.isInteger(size)) throw new Error("Size must be a positive integer.");
	if (step <= 0 || !Number.isInteger(step)) throw new Error("Step must be a positive integer.");
	const result = [];
	const end = partialWindows ? arr.length : arr.length - size + 1;
	for (let i = 0; i < end; i += step) result.push(arr.slice(i, i + size));
	return result;
}
//#endregion
export { windowed };
