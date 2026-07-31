//#region src/fp/array/findLastIndex.ts
/**
* Creates a function that returns the index of the last value matching a predicate.
*
* The predicate receives the value, index, and full input array while scanning from right
* to left. The returned function returns -1 when no value matches.
*
* @template T - The type of elements in the array.
* @param predicate - Called with each value, index, and array from right to left until it returns true.
* @returns A function that maps a readonly array to the last matching index, or -1.
*
* @example
* import { findLastIndex, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 2], findLastIndex(value => value === 2));
* // => 3
*/
function findLastIndex(predicate) {
	return function(array) {
		for (let index = array.length - 1; index >= 0; index--) if (predicate(array[index], index, array)) return index;
		return -1;
	};
}
//#endregion
exports.findLastIndex = findLastIndex;
