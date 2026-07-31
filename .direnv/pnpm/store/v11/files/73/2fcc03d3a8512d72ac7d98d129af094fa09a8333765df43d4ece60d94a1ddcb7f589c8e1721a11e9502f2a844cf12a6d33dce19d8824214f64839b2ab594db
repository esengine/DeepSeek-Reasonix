import { compact as compact$1 } from "../../array/compact.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/compact.ts
/**
* Creates a function that removes falsey values from an array. Use it with {@link pipe}.
*
* Falsey values include false, null, 0, -0, 0n, an empty string, undefined,
* and NaN. The returned function is lazy-capable inside {@link pipe}, so a
* trailing short-circuiting operator such as {@link take} can stop once enough
* truthy values have been emitted.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to a new array with falsey values removed.
*
* @example
* import { compact, pipe } from 'es-toolkit/fp';
*
* pipe([0, 1, false, 2, '', 3], compact());
* // => [1, 2, 3]
*/
function compact() {
	function compactEager(array) {
		return compact$1(array);
	}
	return combineEagerAndLazyFunctions(compactEager, createLazyFunction((value, _index, emit) => {
		if (value) emit(value);
	}));
}
//#endregion
export { compact };
