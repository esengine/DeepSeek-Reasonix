const require_flatMap = require("../../array/flatMap.js");
const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/flatMap.ts
/**
* Creates a function that maps every element to an array with `callback` and
* concatenates the results, equivalent to `Array.prototype.flatMap`. Use it
* with {@link pipe}.
*
* The returned function is **lazy-capable**: inside a {@link pipe} it is fused
* with adjacent lazy functions and runs element-by-element, so a trailing
* `take` can terminate the walk early without expanding the rest of the input.
*
* @template T - The type of elements in the input array.
* @template U - The type of elements in the output array.
* @param callback - Called with `(value, index)` for each element; returns an
*   array whose elements are flattened into the output.
* @returns A function that maps a `readonly T[]` to a new `U[]`.
*
* @example
* import { pipe, flatMap } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], flatMap(x => [x, x * 10])); // => [1, 10, 2, 20, 3, 30]
*
* @example
* // Returning an empty array drops the element.
* pipe([1, 2, 3, 4], flatMap(x => (x % 2 === 0 ? [x] : []))); // => [2, 4]
*/
function flatMap(callback) {
	function flatMapEager(array) {
		return require_flatMap.flatMap(array, callback);
	}
	const flatMapLazy = require_lazy.createLazyFunction((value, index, emit) => {
		const items = callback(value, index);
		for (let i = 0; i < items.length; i++) emit(items[i]);
	});
	return require_lazy.combineEagerAndLazyFunctions(flatMapEager, flatMapLazy);
}
//#endregion
exports.flatMap = flatMap;
