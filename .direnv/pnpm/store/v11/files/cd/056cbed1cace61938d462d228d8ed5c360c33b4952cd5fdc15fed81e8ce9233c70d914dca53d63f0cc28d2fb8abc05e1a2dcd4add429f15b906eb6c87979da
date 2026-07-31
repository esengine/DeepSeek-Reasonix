//#region src/fp/array/join.ts
/**
* Creates a function that joins array values into a string.
*
* The separator is passed to Array.prototype.join, so undefined uses a comma.
* Use the returned function with {@link pipe}.
*
* @template T - The type of elements in the array.
* @param separator - The separator placed between values.
* @returns A function that maps a readonly array to a joined string.
*
* @example
* import { join, pipe } from 'es-toolkit/fp';
*
* pipe(['a', 'b', 'c'], join('-'));
* // => 'a-b-c'
*/
function join(separator) {
	return function(array) {
		return array.join(separator);
	};
}
//#endregion
exports.join = join;
