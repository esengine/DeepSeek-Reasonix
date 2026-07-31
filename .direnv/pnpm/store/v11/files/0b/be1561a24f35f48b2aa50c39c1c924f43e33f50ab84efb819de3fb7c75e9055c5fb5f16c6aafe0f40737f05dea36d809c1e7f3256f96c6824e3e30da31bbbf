//#region src/array/dropRightWhile.ts
/**
* Removes elements from the end of an array until the predicate returns false.
*
* This function iterates over an array from the end and drops elements until the provided
* predicate function returns false. It then returns a new array with the remaining elements.
*
* @template T - The type of elements in the array.
* @param arr - The array from which to drop elements.
* @param canContinueDropping - A predicate function that determines
* whether to continue dropping elements. The function is called with each element from the end,
* and dropping continues as long as it returns true.
* @returns A new array with the elements remaining after the predicate returns false.
*
* @example
* const array = [1, 2, 3, 4, 5];
* const result = dropRightWhile(array, x => x > 3);
* // result will be [1, 2, 3] since elements greater than 3 are dropped from the end.
*/
function dropRightWhile(arr, canContinueDropping) {
	for (let i = arr.length - 1; i >= 0; i--) if (!canContinueDropping(arr[i], i, arr)) return arr.slice(0, i + 1);
	return [];
}
//#endregion
exports.dropRightWhile = dropRightWhile;
