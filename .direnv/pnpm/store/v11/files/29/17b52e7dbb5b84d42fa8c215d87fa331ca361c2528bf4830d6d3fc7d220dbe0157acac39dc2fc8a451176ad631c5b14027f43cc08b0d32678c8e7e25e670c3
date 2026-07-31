import { flatten as flatten$1 } from "../../array/flatten.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/flatten.ts
/**
* Creates a function that flattens an array up to the specified depth.
*
* The returned function is lazy-capable inside {@link pipe}. A trailing
* short-circuiting operator can stop before later nested values are visited.
*
* @template T - The type of elements in the array.
* @template D - The depth to which nested arrays should be flattened.
* @param depth - The flattening depth. Defaults to 1.
* @returns A function that maps the piped array to a flattened array.
*
* @example
* import { flatten, pipe } from 'es-toolkit/fp';
*
* pipe([1, [2, [3]]], flatten(2));
* // => [1, 2, 3]
*/
function flatten(depth = 1) {
	const flooredDepth = Math.floor(depth);
	function flattenEager(array) {
		return flatten$1(array, depth);
	}
	return combineEagerAndLazyFunctions(flattenEager, createLazyFunction((value, _index, emit) => {
		emitFlattened(value, 0, flooredDepth, emit);
	}));
}
function emitFlattened(value, currentDepth, maxDepth, emit) {
	if (Array.isArray(value) && currentDepth < maxDepth) {
		for (let index = 0; index < value.length; index++) emitFlattened(value[index], currentDepth + 1, maxDepth, emit);
		return;
	}
	emit(value);
}
//#endregion
export { flatten };
