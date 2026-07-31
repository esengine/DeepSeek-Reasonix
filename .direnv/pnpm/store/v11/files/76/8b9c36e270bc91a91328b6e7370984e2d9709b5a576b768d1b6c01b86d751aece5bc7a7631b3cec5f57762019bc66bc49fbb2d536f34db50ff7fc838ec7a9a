const require_dropRight = require("../../array/dropRight.js");
//#region src/fp/array/dropRight.ts
/**
* Creates a function that removes a number of values from the end of an array.
*
* The returned function follows the main {@link dropRight} behavior and returns a new array.
* Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @param count - The number of values to drop from the end.
* @returns A function that maps a readonly array to the remaining prefix.
*
* @example
* import { dropRight, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 4], dropRight(2));
* // => [1, 2]
*/
function dropRight(count) {
	return function(array) {
		return require_dropRight.dropRight(array, count);
	};
}
//#endregion
exports.dropRight = dropRight;
