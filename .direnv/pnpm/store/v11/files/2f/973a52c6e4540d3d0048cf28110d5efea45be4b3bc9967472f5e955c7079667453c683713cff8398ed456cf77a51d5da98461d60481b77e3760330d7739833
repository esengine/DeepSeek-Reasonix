import { flatMapDeep as flatMapDeep$1 } from "../../array/flatMapDeep.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/flatMapDeep.ts
/**
* Creates a function that maps each element and recursively flattens the mapped values.
*
* The iteratee receives each value and index. The returned function is lazy-capable inside
* {@link pipe}; a trailing short-circuiting operator can stop before later input values are mapped.
*
* @template T - The type of elements in the input array.
* @template U - The type returned by the iteratee before recursive flattening.
* @param iteratee - Called with each value and index to produce values to flatten.
* @returns A function that maps a readonly array to a deeply flattened array.
*
* @example
* import { flatMapDeep, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2], flatMapDeep(value => [[value, value * 10]]));
* // => [1, 10, 2, 20]
*/
function flatMapDeep(iteratee) {
	function flatMapDeepEager(array) {
		return flatMapDeep$1(array, (item, index) => iteratee(item, index));
	}
	return combineEagerAndLazyFunctions(flatMapDeepEager, createLazyFunction((value, index, emit) => {
		emitDeep(iteratee(value, index), emit);
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
export { flatMapDeep };
