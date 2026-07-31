import { at as at$1 } from "../../array/at.mjs";
//#region src/fp/array/at.ts
/**
* Creates a function that retrieves elements from the piped array at the specified indices.
*
* Negative indices count back from the end of the array, matching the main {@link at}
* implementation. Use the returned function with {@link pipe}.
*
* @template T - The type of elements in the array.
* @param indices - The indices of the elements to retrieve from the piped array.
* @returns A function that maps a readonly array to a new array of selected values.
*
* @example
* import { at, pipe } from 'es-toolkit/fp';
*
* pipe([10, 20, 30, 40], at([1, -1]));
* // => [20, 40]
*/
function at(indices) {
	return function(array) {
		return at$1(array, indices);
	};
}
//#endregion
export { at };
