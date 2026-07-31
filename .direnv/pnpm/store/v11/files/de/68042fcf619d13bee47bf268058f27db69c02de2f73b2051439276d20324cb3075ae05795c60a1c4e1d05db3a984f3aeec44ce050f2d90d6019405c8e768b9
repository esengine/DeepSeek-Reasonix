//#region src/array/zip.ts
/**
* Combines multiple arrays into a single array of tuples.
*
* This function takes multiple arrays and returns a new array where each element is a tuple
* containing the corresponding elements from the input arrays. If the input arrays are of
* different lengths, the resulting array will have the length of the longest input array,
* with undefined values for missing elements.
*
* @template T
* @param arrs - The arrays to zip together.
* @returns A new array of tuples containing the corresponding elements from the input arrays.
*
* @example
* const arr1 = [1, 2, 3];
* const arr2 = ['a', 'b', 'c'];
* const arr3 = [true, false];
* const result = zip(arr1, arr2, arr3);
* // result will be [[1, 'a', true], [2, 'b', false], [3, 'c', undefined]]
*/
function zip(...arrs) {
	let rowCount = 0;
	for (let i = 0; i < arrs.length; i++) if (arrs[i].length > rowCount) rowCount = arrs[i].length;
	const columnCount = arrs.length;
	const result = Array(rowCount);
	for (let i = 0; i < rowCount; ++i) {
		const row = Array(columnCount);
		for (let j = 0; j < columnCount; ++j) row[j] = arrs[j][i];
		result[i] = row;
	}
	return result;
}
//#endregion
export { zip };
