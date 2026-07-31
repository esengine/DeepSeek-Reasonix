import { difference as difference$1 } from "../../array/difference.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
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
		return difference$1(array, secondArray);
	}
	return combineEagerAndLazyFunctions(differenceEager, createLazyFunction((value, _index, emit) => {
		if (!secondSet.has(value)) emit(value);
	}));
}
//#endregion
export { difference };
