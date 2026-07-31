import { sample as sample$1 } from "../../array/sample.mjs";
//#region src/fp/array/sample.ts
/**
* Creates a function that returns one random element from an array.
*
* The returned function follows the main {@link sample} behavior. Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @returns A function that maps a readonly array to a random element.
*
* @example
* import { pipe, sample } from 'es-toolkit/fp';
*
* pipe([1, 2, 3], sample());
* // => one of 1, 2, or 3
*/
function sample() {
	return function(array) {
		return sample$1(array);
	};
}
//#endregion
export { sample };
