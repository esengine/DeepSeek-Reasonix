//#region src/fp/array/length.ts
/**
* Creates a function that returns the length of an array.
*
* Use it with {@link pipe} when a pipeline should end with the number of input values.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to its length.
*
* @example
* import { length, pipe } from 'es-toolkit/fp';
*
* pipe(['a', 'b', 'c'], length());
* // => 3
*/
function length() {
	return function(array) {
		return array.length;
	};
}
//#endregion
exports.length = length;
