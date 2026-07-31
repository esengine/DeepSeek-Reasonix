import { flattenDeep as flattenDeep$1 } from "../../array/flattenDeep.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/flattenDeep.ts
/**
* Creates a function that recursively flattens an array.
*
* The returned function is lazy-capable inside {@link pipe}. A trailing
* short-circuiting operator can stop before later nested values are visited.
*
* @template T - The type of elements in the array.
* @returns A function that maps the piped array to a deeply flattened array.
*
* @example
* import { flattenDeep, pipe } from 'es-toolkit/fp';
*
* pipe([1, [2, [3]]], flattenDeep());
* // => [1, 2, 3]
*/
function flattenDeep() {
	function flattenDeepEager(array) {
		return flattenDeep$1(array);
	}
	return combineEagerAndLazyFunctions(flattenDeepEager, createLazyFunction((value, _index, emit) => {
		emitDeep(value, emit);
	}));
}
function emitDeep(value, emit) {
	if (Array.isArray(value)) {
		for (let index = 0; index < value.length; index++) emitDeep(value[index], emit);
		return;
	}
	emit(value);
}
//#endregion
export { flattenDeep };
