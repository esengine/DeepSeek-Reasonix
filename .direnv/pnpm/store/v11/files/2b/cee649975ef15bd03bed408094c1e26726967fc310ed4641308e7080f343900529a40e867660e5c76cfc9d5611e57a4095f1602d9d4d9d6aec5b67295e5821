import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/forEach.ts
/**
* Creates a function that runs a callback for every element and returns the original array.
*
* This mirrors Remeda's pipeline-friendly `forEach` behavior: the side effect
* runs, then the input continues through the pipeline by reference. The returned
* function is lazy-capable inside {@link pipe}; when followed by a
* short-circuiting operator, the callback only runs for consumed values.
*
* @template T - The type of elements in the array.
* @param callback - Called with each value and index.
* @returns A function that performs the side effect and returns the original array.
*
* @example
* import { forEach, map, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], forEach(value => console.log(value)), map(value => value * 2));
* // => [2, 4, 6]
*/
function forEach(callback) {
	function forEachEager(array) {
		array.forEach(callback);
		return array;
	}
	return combineEagerAndLazyFunctions(forEachEager, createLazyFunction((value, index, emit) => {
		callback(value, index);
		emit(value);
	}));
}
//#endregion
export { forEach };
