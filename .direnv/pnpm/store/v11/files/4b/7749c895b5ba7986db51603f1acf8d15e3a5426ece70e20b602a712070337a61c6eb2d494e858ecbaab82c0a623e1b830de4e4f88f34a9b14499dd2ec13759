const require_sampleSize = require("../../array/sampleSize.js");
const require_toInteger = require("../util/toInteger.js");
const require_isIterateeCall = require("../_internal/isIterateeCall.js");
const require_clamp = require("../math/clamp.js");
const require_toArray = require("../util/toArray.js");
//#region src/compat/array/sampleSize.ts
/**
* Returns a sample element array of a specified `size`.
*
* This function takes an array and a number, and returns an array containing the sampled elements using Floyd's algorithm.
*
* {@link https://www.nowherenearithaca.com/2013/05/robert-floyds-tiny-and-beautiful.html Floyd's algorithm}
*
* @template T - The type of elements in the array.
* @param collection - The array to sample from.
* @param size - The size of sample.
* @returns A new array with sample size applied.
*
* @example
* const result = sampleSize([1, 2, 3], 2)
* // result will be an array containing two of the elements from the collection.
* // [1, 2] or [1, 3] or [2, 3]
*/
function sampleSize(collection, size, guard) {
	const arrayCollection = require_toArray.toArray(collection);
	if (guard ? require_isIterateeCall.isIterateeCall(collection, size, guard) : size === void 0) size = 1;
	else size = require_clamp.clamp(require_toInteger.toInteger(size), 0, arrayCollection.length);
	return require_sampleSize.sampleSize(arrayCollection, size);
}
//#endregion
exports.sampleSize = sampleSize;
