import { without as without$1 } from "../../array/without.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/without.ts
/**
* Creates a function that removes the configured values from an array.
*
* The returned function is lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the array.
* @param values - Values to remove from the piped array.
* @returns A function that maps the piped array to the remaining values.
*
* @example
* import { pipe, without } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 2], without(2));
* // => [1, 3]
*/
function without(...values) {
	const valueSet = new Set(values);
	function withoutEager(array) {
		return without$1(array, ...values);
	}
	return combineEagerAndLazyFunctions(withoutEager, createLazyFunction((value, _index, emit) => {
		if (!valueSet.has(value)) emit(value);
	}));
}
//#endregion
export { without };
