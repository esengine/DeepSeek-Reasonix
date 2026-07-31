const require_differenceBy = require("../../array/differenceBy.js");
const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/differenceBy.ts
/**
* Creates a function that returns values whose mapped identity is absent from another array.
*
* The mapper is applied to values from both arrays. The returned function is
* lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the piped array.
* @template U - The type of elements in the configured array.
* @param secondArray - Values to exclude from the piped array after mapping.
* @param mapper - Maps values from both arrays to comparison keys.
* @returns A function that maps the piped array to its mapped difference.
*
* @example
* import { differenceBy, pipe } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], differenceBy([2], value => typeof value === 'number' ? value : value.id));
* // => [{ id: 1 }]
*/
function differenceBy(secondArray, mapper) {
	const mappedSecondSet = new Set(secondArray.map((item) => mapper(item)));
	function differenceByEager(array) {
		return require_differenceBy.differenceBy(array, secondArray, mapper);
	}
	const differenceByLazy = require_lazy.createLazyFunction((value, _index, emit) => {
		if (!mappedSecondSet.has(mapper(value))) emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(differenceByEager, differenceByLazy);
}
//#endregion
exports.differenceBy = differenceBy;
