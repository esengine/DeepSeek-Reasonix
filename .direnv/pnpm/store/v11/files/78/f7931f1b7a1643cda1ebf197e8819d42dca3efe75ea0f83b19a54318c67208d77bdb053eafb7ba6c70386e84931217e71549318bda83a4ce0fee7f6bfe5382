import { differenceWith as differenceWith$1 } from "../../array/differenceWith.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/differenceWith.ts
/**
* Creates a function that returns values that are not equal to any configured value.
*
* Equality is decided by the provided comparator. The returned function is
* lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the piped array.
* @template U - The type of elements in the configured array.
* @param secondArray - Values to compare against the piped array.
* @param areItemsEqual - Returns true when a piped value equals a configured value.
* @returns A function that maps the piped array to its custom difference.
*
* @example
* import { differenceWith, pipe } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], differenceWith([2], (item, id) => item.id === id));
* // => [{ id: 1 }]
*/
function differenceWith(secondArray, areItemsEqual) {
	function differenceWithEager(array) {
		return differenceWith$1(array, secondArray, areItemsEqual);
	}
	return combineEagerAndLazyFunctions(differenceWithEager, createLazyFunction((value, _index, emit) => {
		if (secondArray.every((other) => !areItemsEqual(value, other))) emit(value);
	}));
}
//#endregion
export { differenceWith };
