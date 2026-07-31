//#region src/array/dropRight.ts
/**
* Removes a specified number of elements from the end of an array and returns the rest.
*
* This function takes an array and a number, and returns a new array with the specified number
* of elements removed from the end.
*
* @template T - The type of elements in the array.
* @param arr - The array from which to drop elements.
* @param itemsCount - The number of elements to drop from the end of the array.
* @returns A new array with the specified number of elements removed from the end.
*
* @example
* const array = [1, 2, 3, 4, 5];
* const result = dropRight(array, 2);
* // result will be [1, 2, 3] since the last two elements are dropped.
*/
function dropRight(arr, itemsCount) {
	itemsCount = Math.min(-itemsCount, 0);
	if (itemsCount === 0) return arr.slice();
	return arr.slice(0, itemsCount);
}
//#endregion
export { dropRight };
