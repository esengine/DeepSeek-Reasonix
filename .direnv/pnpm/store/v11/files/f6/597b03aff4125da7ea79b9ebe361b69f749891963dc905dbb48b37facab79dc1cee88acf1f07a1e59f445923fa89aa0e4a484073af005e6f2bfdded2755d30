const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
const require_sortedIndex = require("./sortedIndex.js");
//#region src/compat/array/sortedIndexOf.ts
/**
* Finds the index of the first occurrence of a value in a sorted array, similar to how `Array#indexOf` works, but specifically for sorted arrays.
*
* Make sure to provide a sorted array to this function, as it uses a binary search to quickly find the index.
*
* @param array The sorted array to inspect.
* @param value The value to search for.
* @returns Returns the index of the matched value, else -1.
*
* @example
* const numbers = [1, 2, 3, 4, 5];
* sortedIndexOf(numbers, 11); // Return value: 0
* sortedIndexOf(numbers, 30); // Return value: -1
*
* // If the value is duplicated, it returns the first index of the value.
* const duplicateNumbers = [1, 2, 2, 3, 3, 3, 4];
* sortedIndexOf(duplicateNumbers, 3); // Return value: 3
*
* // If the array is unsorted, it can return the wrong index.
* const unSortedArray = [55, 33, 22, 11, 44];
* sortedIndexOf(unSortedArray, 11); // Return value: -1
*
* // -0 and 0 are treated the same
* const mixedZeroArray = [-0, 0];
* sortedIndexOf(mixedZeroArray, 0); // Return value: 0
* sortedIndexOf(mixedZeroArray, -0); // Return value: 0
*
* // It works with array-like objects
* const arrayLike = { length: 3, 0: 10, 1: 20, 2: 30 };
* sortedIndexOf(arrayLike, 20); // Return value: 1
*/
function sortedIndexOf(array, value) {
	if (!array?.length) return -1;
	const index = require_sortedIndex.sortedIndex(array, value);
	if (index < array.length && require_isEqualsSameValueZero.isEqualsSameValueZero(array[index], value)) return index;
	return -1;
}
//#endregion
exports.sortedIndexOf = sortedIndexOf;
