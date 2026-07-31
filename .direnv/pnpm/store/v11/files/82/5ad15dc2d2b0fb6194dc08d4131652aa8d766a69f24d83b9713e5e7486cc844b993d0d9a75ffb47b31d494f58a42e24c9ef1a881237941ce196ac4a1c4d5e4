//#region src/array/at.ts
/**
* Retrieves elements from an array at the specified indices.
*
* This function supports negative indices, which count from the end of the array.
*
* @template T
* @param arr - The array to retrieve elements from.
* @param indices - An array of indices specifying the positions of elements to retrieve.
* @returns A new array containing the elements at the specified indices.
*
* @example
* const numbers = [10, 20, 30, 40, 50];
* const result = at(numbers, [1, 3, 4]);
* console.log(result); // [20, 40, 50]
*/
function at(arr, indices) {
	const result = new Array(indices.length);
	const length = arr.length;
	for (let i = 0; i < indices.length; i++) {
		let index = indices[i];
		index = Number.isInteger(index) ? index : Math.trunc(index) || 0;
		if (index < 0) index += length;
		result[i] = arr[index];
	}
	return result;
}
//#endregion
export { at };
