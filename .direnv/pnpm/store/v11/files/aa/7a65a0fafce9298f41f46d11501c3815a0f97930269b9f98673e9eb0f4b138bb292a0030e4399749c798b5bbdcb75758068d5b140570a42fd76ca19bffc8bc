const require_differenceWith = require("../../array/differenceWith.js");
const require_lazy = require("../_internal/lazy.js");
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
		return require_differenceWith.differenceWith(array, secondArray, areItemsEqual);
	}
	const differenceWithLazy = require_lazy.createLazyFunction((value, _index, emit) => {
		if (secondArray.every((other) => !areItemsEqual(value, other))) emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(differenceWithEager, differenceWithLazy);
}
//#endregion
exports.differenceWith = differenceWith;
