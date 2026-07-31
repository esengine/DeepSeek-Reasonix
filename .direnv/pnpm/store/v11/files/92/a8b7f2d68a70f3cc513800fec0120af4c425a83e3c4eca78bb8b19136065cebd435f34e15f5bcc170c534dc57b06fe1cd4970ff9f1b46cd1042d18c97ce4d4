//#region src/array/uniqWith.ts
/**
* Returns a new array containing only the unique elements from the original array,
* based on the values returned by the comparator function.
*
* @template T - The type of elements in the array.
* @param arr - The array to process.
* @param areItemsEqual - The function used to compare the array elements.
* @returns A new array containing only the unique elements from the original array, based on the values returned by the comparator function.
*
* @example
* ```ts
* uniqWith([1.2, 1.5, 2.1, 3.2, 5.7, 5.3, 7.19], (a, b) => Math.abs(a - b) < 1);
* // [1.2, 3.2, 5.7, 7.19]
* ```
*/
function uniqWith(arr, areItemsEqual) {
	const result = [];
	for (let i = 0; i < arr.length; i++) {
		const item = arr[i];
		if (result.every((v) => !areItemsEqual(v, item))) result.push(item);
	}
	return result;
}
//#endregion
export { uniqWith };
