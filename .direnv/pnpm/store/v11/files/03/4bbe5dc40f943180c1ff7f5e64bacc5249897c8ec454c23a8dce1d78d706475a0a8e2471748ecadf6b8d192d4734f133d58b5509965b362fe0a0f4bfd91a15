import { unzipWith as unzipWith$1 } from "../../array/unzipWith.mjs";
//#region src/fp/array/unzipWith.ts
/**
* Creates a function that unzips grouped arrays and combines values by position.
*
* Values at the same position are passed to iteratee, and its return values form the
* resulting array.
*
* @template T - The element type inside each grouped array.
* @template R - The value returned by the iteratee.
* @param iteratee - Called with the values at each position.
* @returns A function that maps grouped arrays to combined values.
*
* @example
* import { pipe, unzipWith } from 'es-toolkit/fp';
*
* pipe([[1, 10], [2, 20]], unzipWith((a, b) => a + b));
* // => [3, 30]
*/
function unzipWith(iteratee) {
	return function(target) {
		return unzipWith$1(target, iteratee);
	};
}
//#endregion
export { unzipWith };
