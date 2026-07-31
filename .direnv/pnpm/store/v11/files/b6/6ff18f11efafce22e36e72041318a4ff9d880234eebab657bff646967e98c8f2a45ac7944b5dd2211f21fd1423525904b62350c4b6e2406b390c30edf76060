//#region src/array/unzipWith.ts
/**
* Unzips an array of arrays, applying an `iteratee` function to regrouped elements.
*
* @template T, R
* @param target - The nested array to unzip. This is an array of arrays,
* where each inner array contains elements to be unzipped.
* @param iteratee - A function to transform the unzipped elements.
* @returns A new array of unzipped and transformed elements.
*
* @example
* const nestedArray = [[1, 2], [3, 4], [5, 6]];
* const result = unzipWith(nestedArray, (item, item2, item3) => item + item2 + item3);
* // result will be [9, 12]
*/
function unzipWith(target, iteratee) {
	const maxLength = Math.max(...target.map((innerArray) => innerArray.length));
	const result = new Array(maxLength);
	for (let i = 0; i < maxLength; i++) {
		const group = new Array(target.length);
		for (let j = 0; j < target.length; j++) group[j] = target[j][i];
		result[i] = iteratee(...group);
	}
	return result;
}
//#endregion
export { unzipWith };
