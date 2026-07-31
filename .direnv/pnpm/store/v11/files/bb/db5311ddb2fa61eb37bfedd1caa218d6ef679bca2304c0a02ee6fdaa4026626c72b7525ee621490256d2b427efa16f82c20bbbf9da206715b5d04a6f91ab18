const require_flattenDeep = require("../../array/flattenDeep.js");
const require_lazy = require("../_internal/lazy.js");
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
		return require_flattenDeep.flattenDeep(array);
	}
	const flattenDeepLazy = require_lazy.createLazyFunction((value, _index, emit) => {
		emitDeep(value, emit);
	});
	return require_lazy.combineEagerAndLazyFunctions(flattenDeepEager, flattenDeepLazy);
}
function emitDeep(value, emit) {
	if (Array.isArray(value)) {
		for (let index = 0; index < value.length; index++) emitDeep(value[index], emit);
		return;
	}
	emit(value);
}
//#endregion
exports.flattenDeep = flattenDeep;
