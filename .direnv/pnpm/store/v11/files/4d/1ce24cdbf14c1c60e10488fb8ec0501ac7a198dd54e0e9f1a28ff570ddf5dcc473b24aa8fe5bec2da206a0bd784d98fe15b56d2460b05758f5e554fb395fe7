const require_xor = require("../../array/xor.js");
//#region src/fp/array/xor.ts
/**
* Creates a function that returns the symmetric difference with secondArray.
*
* The result contains values that appear in exactly one of the arrays, matching the main
* {@link xor} behavior.
*
* @template T - The type of elements in the arrays.
* @param secondArray - Values to compare with the piped array.
* @returns A function that maps a readonly array to its symmetric difference.
*
* @example
* import { pipe, xor } from 'es-toolkit/fp';
*
* pipe([1, 2], xor([2, 3]));
* // => [1, 3]
*/
function xor(secondArray) {
	return function(array) {
		return require_xor.xor(array, secondArray);
	};
}
//#endregion
exports.xor = xor;
