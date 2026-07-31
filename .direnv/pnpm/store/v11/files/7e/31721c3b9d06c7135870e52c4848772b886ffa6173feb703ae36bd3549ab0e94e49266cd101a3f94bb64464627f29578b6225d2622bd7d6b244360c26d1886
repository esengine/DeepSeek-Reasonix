const require_initial = require("../../array/initial.js");
//#region src/fp/array/initial.ts
/**
* Creates a function that returns every element except the last one.
*
* The returned function follows the main {@link initial} behavior and always returns a new array.
* Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to its initial elements.
*
* @example
* import { initial, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], initial());
* // => [1, 2]
*/
function initial() {
	return function(array) {
		return require_initial.initial(array);
	};
}
//#endregion
exports.initial = initial;
