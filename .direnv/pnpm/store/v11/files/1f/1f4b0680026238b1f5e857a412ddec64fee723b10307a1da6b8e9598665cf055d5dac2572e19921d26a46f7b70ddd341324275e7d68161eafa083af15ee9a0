const require_toFilled = require("../../array/toFilled.js");
//#region src/fp/array/toFilled.ts
/**
* Creates a function that returns a filled copy of an array.
*
* The returned function follows Array.prototype.fill indexing semantics through the main
* {@link toFilled} implementation and does not mutate the input array.
*
* @template T - The type of elements in the input array.
* @template U - The type of the fill value.
* @param value - The value to write into the returned array.
* @param start - The start index. Defaults to 0.
* @param end - The end index. Defaults to the array length.
* @returns A function that maps a readonly array to a filled copy.
*
* @example
* import { pipe, toFilled } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], toFilled(0, 1));
* // => [1, 0, 0]
*/
function toFilled(value, start, end) {
	return function(array) {
		if (start == null) return require_toFilled.toFilled(array, value);
		if (end == null) return require_toFilled.toFilled(array, value, start);
		return require_toFilled.toFilled(array, value, start, end);
	};
}
//#endregion
exports.toFilled = toFilled;
