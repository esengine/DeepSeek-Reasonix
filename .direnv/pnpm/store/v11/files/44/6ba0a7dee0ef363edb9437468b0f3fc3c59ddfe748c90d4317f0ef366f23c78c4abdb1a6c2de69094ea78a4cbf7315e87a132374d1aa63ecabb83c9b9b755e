import { intersectionWith as intersectionWith$1 } from "../../array/intersectionWith.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/intersectionWith.ts
/**
* Creates a function that keeps values equal to at least one configured value.
*
* Equality is decided by the provided comparator. The returned function is
* lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the piped array.
* @template U - The type of elements in the configured array.
* @param secondArray - Values to compare against the piped array.
* @param areItemsEqual - Returns true when a piped value equals a configured value.
* @returns A function that maps the piped array to its custom intersection.
*
* @example
* import { intersectionWith, pipe } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], intersectionWith([2], (item, id) => item.id === id));
* // => [{ id: 2 }]
*/
function intersectionWith(secondArray, areItemsEqual) {
	function intersectionWithEager(array) {
		return intersectionWith$1(array, secondArray, areItemsEqual);
	}
	return combineEagerAndLazyFunctions(intersectionWithEager, createLazyFunction((value, _index, emit) => {
		if (secondArray.some((other) => areItemsEqual(value, other))) emit(value);
	}));
}
//#endregion
export { intersectionWith };
