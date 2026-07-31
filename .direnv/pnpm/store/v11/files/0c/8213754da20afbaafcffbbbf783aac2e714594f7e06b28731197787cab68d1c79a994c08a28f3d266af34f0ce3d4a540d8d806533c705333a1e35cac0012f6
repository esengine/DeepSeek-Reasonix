const require_shuffle = require("../../array/shuffle.js");
//#region src/fp/array/shuffle.ts
/**
* Creates a function that returns a shuffled copy of an array.
*
* The returned function uses the main {@link shuffle} implementation and does not mutate
* the input array. Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to a shuffled copy.
*
* @example
* import { pipe, shuffle } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], shuffle());
* // => the same values in random order
*/
function shuffle() {
	return function(array) {
		return require_shuffle.shuffle(array);
	};
}
//#endregion
exports.shuffle = shuffle;
