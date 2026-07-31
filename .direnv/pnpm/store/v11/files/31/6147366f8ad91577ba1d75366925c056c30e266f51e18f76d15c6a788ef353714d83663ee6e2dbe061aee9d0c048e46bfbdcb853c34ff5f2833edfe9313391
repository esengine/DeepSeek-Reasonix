import { takeWhile as takeWhile$1 } from "../../array/takeWhile.mjs";
import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/takeWhile.ts
/**
* Creates a function that takes leading values while a predicate returns true.
*
* The returned function is lazy-capable and short-circuiting inside
* {@link pipe}; once the predicate returns false, upstream lazy operators stop
* processing further input.
*
* @template T - The type of elements in the array.
* @param predicate - Called with each value and index until it returns false.
* @returns A function that maps the piped array to the matching prefix.
*
* @example
* import { pipe, takeWhile } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 1], takeWhile(value => value < 3));
* // => [1, 2]
*/
function takeWhile(predicate) {
	function takeWhileEager(array) {
		return takeWhile$1(array, (element, index) => predicate(element, index));
	}
	return combineEagerAndLazyFunctions(takeWhileEager, createLazyFunction((value, index, emit) => {
		if (!predicate(value, index)) return false;
		emit(value);
	}), { shortCircuit: true });
}
//#endregion
export { takeWhile };
