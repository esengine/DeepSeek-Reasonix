const require_chunk = require("../../array/chunk.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/chunk.ts
/**
* Splits an array into smaller arrays of a specified length.
*
* This function takes an input array and divides it into multiple smaller arrays,
* each of a specified length. If the input array cannot be evenly divided,
* the final sub-array will contain the remaining elements.
*
* @template T The type of elements in the array.
* @param arr - The array to be chunked into smaller arrays.
* @param size - The size of each smaller array. Must be a positive integer.
* @returns A two-dimensional array where each sub-array has a maximum length of `size`.
*
* @example
* // Splits an array of numbers into sub-arrays of length 2
* chunk([1, 2, 3, 4, 5], 2);
* // Returns: [[1, 2], [3, 4], [5]]
*
* @example
* // Splits an array of strings into sub-arrays of length 3
* chunk(['a', 'b', 'c', 'd', 'e', 'f', 'g'], 3);
* // Returns: [['a', 'b', 'c'], ['d', 'e', 'f'], ['g']]
*/
function chunk(arr, size = 1) {
	size = Math.max(Math.floor(size), 0);
	if (size === 0 || !require_isArrayLike.isArrayLike(arr) || Number.isNaN(size)) return [];
	const array = require_toArray.toArray(arr);
	if (!isFinite(size)) return [array];
	return require_chunk.chunk(array, size);
}
//#endregion
exports.chunk = chunk;
