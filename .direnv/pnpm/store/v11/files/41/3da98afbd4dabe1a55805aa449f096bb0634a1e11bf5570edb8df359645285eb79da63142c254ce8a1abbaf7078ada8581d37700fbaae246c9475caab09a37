//#region src/array/unzip.ts
/**
* Gathers elements in the same position in an internal array
* from a grouped array of elements and returns them as a new array.
*
* @template T - The type of elements in the nested array.
* @param zipped - The nested array to unzip.
* @returns A new array of unzipped elements.
*
* @example
* const zipped = [['a', true, 1],['b', false, 2]];
* const result = unzip(zipped);
* // result will be [['a', 'b'], [true, false], [1, 2]]
*/
function unzip(zipped) {
	let maxLen = 0;
	for (let i = 0; i < zipped.length; i++) if (zipped[i].length > maxLen) maxLen = zipped[i].length;
	const result = new Array(maxLen);
	for (let i = 0; i < maxLen; i++) {
		result[i] = new Array(zipped.length);
		for (let j = 0; j < zipped.length; j++) result[i][j] = zipped[j][i];
	}
	return result;
}
//#endregion
export { unzip };
