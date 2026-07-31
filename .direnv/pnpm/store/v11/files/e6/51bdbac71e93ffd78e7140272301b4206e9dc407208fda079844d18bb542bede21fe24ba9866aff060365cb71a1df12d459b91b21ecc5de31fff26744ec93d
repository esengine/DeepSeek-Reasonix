const require_combinations = require("../../array/combinations.js");
//#region src/fp/array/combinations.ts
/**
* Creates a function that returns every combination of the configured size.
*
* Elements are treated as unique by position, so duplicate values can produce identical-looking
* combinations. The returned function throws when size is not a non-negative integer.
*
* @template T - The type of elements in the array.
* @param size - The number of items in each combination.
* @returns A function that maps a readonly array to its size-length combinations.
* @throws {Error} When size is not a non-negative integer.
*
* @example
* import { combinations, pipe } from 'es-toolkit/fp';
*
* pipe(['A', 'B', 'C'], combinations(2));
* // => [['A', 'B'], ['A', 'C'], ['B', 'C']]
*/
function combinations(size) {
	return function(array) {
		return require_combinations.combinations(array, size);
	};
}
//#endregion
exports.combinations = combinations;
