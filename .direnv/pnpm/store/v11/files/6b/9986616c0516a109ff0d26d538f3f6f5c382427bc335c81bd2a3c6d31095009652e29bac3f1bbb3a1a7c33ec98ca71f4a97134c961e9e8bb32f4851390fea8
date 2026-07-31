const require_takeWhile = require("../../array/takeWhile.js");
const require_lazy = require("../_internal/lazy.js");
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
		return require_takeWhile.takeWhile(array, (element, index) => predicate(element, index));
	}
	const takeWhileLazy = require_lazy.createLazyFunction((value, index, emit) => {
		if (!predicate(value, index)) return false;
		emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(takeWhileEager, takeWhileLazy, { shortCircuit: true });
}
//#endregion
exports.takeWhile = takeWhile;
