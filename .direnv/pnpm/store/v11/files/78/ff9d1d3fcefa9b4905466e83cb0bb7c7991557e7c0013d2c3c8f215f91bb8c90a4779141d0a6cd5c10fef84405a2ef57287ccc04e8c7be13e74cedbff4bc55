const require_flattenDeep = require("./flattenDeep.js");
//#region src/array/flatMapDeep.ts
/**
* Recursively maps each element in an array using a provided iteratee function and then deeply flattens the resulting array.
*
* @template T - The type of elements within the array.
* @template U - The type of elements within the returned array from the iteratee function.
* @param arr - The array to flatten.
* @param iteratee - The function that produces the new array elements. It receives the element, its index, and the array.
* @returns A new array that has been flattened.
*
* @example
* const result = flatMapDeep([1, 2, 3], n => [[n, n]]);
* // [1, 1, 2, 2, 3, 3]
*/
function flatMapDeep(arr, iteratee) {
	return require_flattenDeep.flattenDeep(arr.map((item, index) => iteratee(item, index, arr)));
}
//#endregion
exports.flatMapDeep = flatMapDeep;
