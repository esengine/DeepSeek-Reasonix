import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/map.ts
/**
* Creates a function that builds a new array by calling `callback` on every
* element, equivalent to `Array.prototype.map`. Use it with {@link pipe}.
*
* The returned function is **lazy-capable**: inside a {@link pipe} it is fused
* with adjacent lazy functions and runs element-by-element, so a trailing
* `take` can terminate the walk early without mapping the rest of the input.
*
* @template T - The type of elements in the input array.
* @template U - The type of elements in the output array.
* @param callback - Called with `(value, index)` for each element; its return
*   value becomes the corresponding element of the output array.
* @returns A function that maps a `readonly T[]` to a new `U[]`.
*
* @example
* import { pipe, map } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], map(x => x * 2)); // => [2, 4, 6]
*
* @example
* // The index is available as the second argument.
* pipe([10, 20, 30], map((value, index) => value + index)); // => [10, 21, 32]
*/
function map(callback) {
	function mapEager(array) {
		return array.map(callback);
	}
	return combineEagerAndLazyFunctions(mapEager, createLazyFunction((value, index, emit) => {
		emit(callback(value, index));
	}));
}
//#endregion
export { map };
