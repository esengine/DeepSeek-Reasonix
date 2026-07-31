//#region src/fp/array/findIndex.ts
/**
* Creates a function that returns the index of the first value matching a predicate.
*
* The predicate receives the value, index, and full input array. The returned function
* returns -1 when no value matches, matching Array.prototype.findIndex.
*
* @template T - The type of elements in the array.
* @param predicate - Called with each value, index, and array until it returns true.
* @returns A function that maps a readonly array to the first matching index, or -1.
*
* @example
* import { findIndex, pipe } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], findIndex(item => item.id === 2));
* // => 1
*/
function findIndex(predicate) {
	return function(array) {
		return array.findIndex(predicate);
	};
}
//#endregion
export { findIndex };
