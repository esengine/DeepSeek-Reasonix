import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import { sortedLastIndex } from "./sortedLastIndex.mjs";
//#region src/compat/array/sortedLastIndexOf.ts
/**
* Finds the index of the last occurrence of a value in a sorted array.
* This function is similar to `Array#lastIndexOf` but is specifically designed for sorted arrays.
*
* Make sure to provide a sorted array to this function, as it uses a binary search to quickly find the index.
*
* @param array The sorted array to inspect.
* @param value The value to search for.
* @returns Returns the index of the last matched value, else -1.
*
* @example
* const numbers = [1, 2, 3, 4, 5];
* sortedLastIndexOf(numbers, 3); // Return value: 2
* sortedLastIndexOf(numbers, 6); // Return value: -1
*
* // If the value is duplicated, it returns the last index of the value.
* const duplicateNumbers = [1, 2, 2, 3, 3, 3, 4];
* sortedLastIndexOf(duplicateNumbers, 3); // Return value: 5
*
* // If the array is unsorted, it can return the wrong index.
* const unSortedArray = [55, 33, 22, 11, 44];
* sortedLastIndexOf(unSortedArray, 11); // Return value: -1
*
* // -0 and 0 are treated the same
* const mixedZeroArray = [-0, 0];
* sortedLastIndexOf(mixedZeroArray, 0); // Return value: 1
* sortedLastIndexOf(mixedZeroArray, -0); // Return value: 1
*
* // It works with array-like objects
* const arrayLike = { length: 3, 0: 10, 1: 20, 2: 20 };
* sortedLastIndexOf(arrayLike, 20); // Return value: 2
*/
function sortedLastIndexOf(array, value) {
	if (!array?.length) return -1;
	const index = sortedLastIndex(array, value) - 1;
	if (index >= 0 && isEqualsSameValueZero(array[index], value)) return index;
	return -1;
}
//#endregion
export { sortedLastIndexOf };
