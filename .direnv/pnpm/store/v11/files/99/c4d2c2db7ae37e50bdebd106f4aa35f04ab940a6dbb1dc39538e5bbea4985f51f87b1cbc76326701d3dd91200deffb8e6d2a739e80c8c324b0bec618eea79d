import { unzip } from "../../array/unzip.mjs";
import { isArray } from "../predicate/isArray.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
//#region src/compat/array/unzipWith.ts
/**
* Unzips an array of arrays, applying an `iteratee` function to regrouped elements.
*
* If the array is `null` or `undefined`, returns an empty array.
*
* @template T
* @param array - The nested array to unzip. This is an array of arrays,
* where each inner array contains elements to be unzipped.
* @param iteratee - A function to transform the unzipped elements.
* @returns A new array of unzipped and transformed elements.
*
* @example
* const nestedArray = [[1, 2], [3, 4], [5, 6]];
* const result = unzipWith(nestedArray, (a, b) => a + b);
* console.log(result); // [9, 12]
*/
function unzipWith(array, iteratee) {
	if (!isArrayLikeObject(array) || !array.length) return [];
	const unzipped = isArray(array) ? unzip(array) : unzip(Array.from(array, (value) => Array.from(value)));
	if (!iteratee) return unzipped;
	const result = new Array(unzipped.length);
	for (let i = 0; i < unzipped.length; i++) {
		const value = unzipped[i];
		result[i] = iteratee(...value);
	}
	return result;
}
//#endregion
export { unzipWith };
