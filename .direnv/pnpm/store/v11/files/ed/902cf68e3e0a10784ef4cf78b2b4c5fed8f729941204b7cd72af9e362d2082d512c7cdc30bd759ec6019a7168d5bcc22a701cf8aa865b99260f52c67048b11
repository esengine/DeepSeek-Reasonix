const require_difference = require("../../array/difference.js");
const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/difference.ts
/**
* Creates a function that returns values from the piped array that are not present in another array.
*
* Equality follows SameValueZero through Set membership, matching the main
* {@link difference} implementation. The returned function is lazy-capable
* inside {@link pipe}.
*
* @template T - The type of elements in the arrays.
* @param secondArray - Values to exclude from the piped array.
* @returns A function that maps the piped array to its difference.
*
* @example
* import { difference, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], difference([2, 4]));
* // => [1, 3]
*/
function difference(secondArray) {
	const secondSet = new Set(secondArray);
	function differenceEager(array) {
		return require_difference.difference(array, secondArray);
	}
	const differenceLazy = require_lazy.createLazyFunction((value, _index, emit) => {
		if (!secondSet.has(value)) emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(differenceEager, differenceLazy);
}
//#endregion
exports.difference = difference;
