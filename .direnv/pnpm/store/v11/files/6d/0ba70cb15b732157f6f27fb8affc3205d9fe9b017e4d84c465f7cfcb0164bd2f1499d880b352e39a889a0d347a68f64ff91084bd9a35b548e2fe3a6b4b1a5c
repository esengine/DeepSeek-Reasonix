const require_sample = require("../../array/sample.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/sample.ts
/**
* The implementation for the overloaded sample function.
*
* This function takes an array, string, or object and returns a single element selected randomly.
* If the input is empty, or if it's null or undefined, the function returns `undefined`.
*
* @template T - The type of elements in the collection.
* @param collection - The collection to sample from.
* @returns A random element from the collection, or `undefined` if the collection is empty or invalid.
*/
function sample(collection) {
	if (collection == null) return;
	if (require_isArrayLike.isArrayLike(collection)) return require_sample.sample(require_toArray.toArray(collection));
	return require_sample.sample(Object.values(collection));
}
//#endregion
exports.sample = sample;
