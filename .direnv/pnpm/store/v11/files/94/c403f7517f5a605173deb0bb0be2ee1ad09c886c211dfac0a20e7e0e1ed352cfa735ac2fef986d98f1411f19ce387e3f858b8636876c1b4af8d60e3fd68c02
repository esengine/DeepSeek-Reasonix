//#region src/array/intersection.ts
/**
* Returns the intersection of two arrays.
*
* This function takes two arrays and returns a new array containing the elements that are
* present in both arrays. It effectively filters out any elements from the first array that
* are not found in the second array.
*
* @template T - The type of elements in the array.
* @param firstArr - The first array to compare.
* @param secondArr - The second array to compare.
* @returns A new array containing the elements that are present in both arrays.
*
* @example
* const array1 = [1, 2, 3, 4, 5];
* const array2 = [3, 4, 5, 6, 7];
* const result = intersection(array1, array2);
* // result will be [3, 4, 5] since these elements are in both arrays.
*/
function intersection(firstArr, secondArr) {
	const secondSet = new Set(secondArr);
	return firstArr.filter((item) => secondSet.has(item));
}
//#endregion
export { intersection };
