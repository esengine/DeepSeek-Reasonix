import { intersection as intersection$1 } from "../../array/intersection.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
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
		return intersection$1(array, secondArray);
	}
	return combineEagerAndLazyFunctions(intersectionEager, createLazyFunction((value, _index, emit) => {
		if (secondSet.has(value)) emit(value);
	}));
}
//#endregion
export { intersection };
