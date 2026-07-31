const require_isFunction = require("../../predicate/isFunction.js");
const require_unzip = require("./unzip.js");
//#region src/compat/array/zipWith.ts
/**
* Combines multiple arrays into a single array using a custom combiner function.
*
* This function takes one array and a variable number of additional arrays,
* applying the provided combiner function to the corresponding elements of each array.
* If the input arrays are of different lengths, the resulting array will have the length
* of the longest input array, with undefined values for missing elements.
*
* @template T - The type of elements in the input arrays.
* @template R - The type of elements in the resulting array.
* @param combine - The combiner function that takes corresponding elements from each array and returns a single value.
* @returns A new array where each element is the result of applying the combiner function to the corresponding elements of the input arrays.
*
* @example
* const arr1 = [1, 2, 3];
* const arr2 = ['a', 'b', 'c'];
* const result = zipWith(arr1, arr2, (num, char) => `${num}${char}`);
* // result will be ['1a', '2b', '3c']
*/
function zipWith(...combine) {
	let iteratee = combine.pop();
	if (!require_isFunction.isFunction(iteratee)) {
		combine.push(iteratee);
		iteratee = void 0;
	}
	if (!combine?.length) return [];
	const result = require_unzip.unzip(combine);
	if (iteratee == null) return result;
	return result.map((group) => iteratee(...group));
}
//#endregion
exports.zipWith = zipWith;
