//#region src/fp/array/reverse.ts
/**
* Creates a function that returns a reversed copy of an array.
*
* Unlike Array.prototype.reverse, the returned function does not mutate the input array.
* Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to a reversed copy.
*
* @example
* import { pipe, reverse } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], reverse());
* // => [3, 2, 1]
*/
function reverse() {
	return function(array) {
		return array.slice().reverse();
	};
}
//#endregion
exports.reverse = reverse;
