import { sampleSize as sampleSize$1 } from "../../array/sampleSize.mjs";
//#region src/fp/array/sampleSize.ts
/**
* Creates a function that returns random elements from an array.
*
* The returned function follows the main {@link sampleSize} behavior and returns a new array.
* Use it with {@link pipe}.
*
* @template T - The type of elements in the array.
* @param size - The number of elements to sample.
* @returns A function that maps a readonly array to sampled elements.
*
* @example
* import { pipe, sampleSize } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 4], sampleSize(2));
* // => two random elements
*/
function sampleSize(size) {
	return function(array) {
		return sampleSize$1(array, size);
	};
}
//#endregion
export { sampleSize };
