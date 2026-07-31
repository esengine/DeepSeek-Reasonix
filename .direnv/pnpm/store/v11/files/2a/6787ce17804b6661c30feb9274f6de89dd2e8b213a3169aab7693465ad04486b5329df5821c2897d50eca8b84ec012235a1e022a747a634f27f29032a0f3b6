const require_intersection = require("../../array/intersection.js");
const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/intersection.ts
/**
* Creates a function that keeps values from the piped array that are also present in another array.
*
* Equality follows SameValueZero through Set membership, matching the main
* {@link intersection} implementation. The returned function is lazy-capable
* inside {@link pipe}.
*
* @template T - The type of elements in the arrays.
* @param secondArray - Values to intersect with the piped array.
* @returns A function that maps the piped array to its intersection.
*
* @example
* import { intersection, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], intersection([2, 4]));
* // => [2]
*/
function intersection(secondArray) {
	const secondSet = new Set(secondArray);
	function intersectionEager(array) {
		return require_intersection.intersection(array, secondArray);
	}
	const intersectionLazy = require_lazy.createLazyFunction((value, _index, emit) => {
		if (secondSet.has(value)) emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(intersectionEager, intersectionLazy);
}
//#endregion
exports.intersection = intersection;
